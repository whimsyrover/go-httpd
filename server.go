package httpd

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/rsms/go-httpd/session"
	"github.com/rsms/go-log"
	"github.com/rsms/gotalk"
)

type Server struct {
	Gotalk   *gotalk.WebSocketServer
	Logger   *log.Logger   // defaults to log.RootLogger
	PubDir   string        // directory to serve files from. File serving is disabled if empty.
	Routes   http.ServeMux // http request routes
	Server   http.Server   // underlying http server
	Sessions session.Store // Call Sessions.SetStorage(s) to enable sessions

	fileHandler http.Handler // serves pubdir (nil if len(PubDir)==0)

	gotalkSocksMu       sync.RWMutex                 // protects gotalkSocks field
	gotalkSocks         map[*gotalk.WebSocket]int    // currently connected gotalk sockets
	gotalkOnConnectUser func(sock *gotalk.WebSocket) // saved value of .Gotalk.OnConnect

	gracefulShutdownTimeout time.Duration
}

func NewServer(pubDir, addr string) *Server {
	s := &Server{
		PubDir: pubDir,
		Gotalk: gotalk.WebSocketHandler(),
		Logger: log.RootLogger,

		Server: http.Server{
			Addr:           addr,
			WriteTimeout:   10 * time.Second,
			ReadTimeout:    10 * time.Second,
			MaxHeaderBytes: 1 << 20, // 1MB
		},
	}

	if len(pubDir) > 0 {
		s.fileHandler = http.FileServer(http.Dir(pubDir))
	}

	s.Server.Handler = &s.Routes
	s.Gotalk.Handlers = gotalk.NewHandlers()
	s.Server.RegisterOnShutdown(func() {
		s.logi("s.Server.RegisterOnShutdown")
		// close all connected sockets
		s.gotalkSocksMu.RLock()
		defer s.gotalkSocksMu.RUnlock()
		for s := range s.gotalkSocks {
			s.Close()
		}
	})

	// XXX DEBUG HTTP routes
	// s.Routes.HandleFunc("/hello", func(w http.ResponseWriter, req *http.Request) {
	// 	// // The "/" pattern matches everything, so we need to check
	// 	// // that we're at the root here.
	// 	// if req.URL.Path != "/hello" {
	// 	// 	http.NotFound(w, req)
	// 	// 	return
	// 	// }
	// 	fmt.Fprintf(w, "Welcome to the home page!")
	// })

	s.Routes.Handle("/gotalk/", s.Gotalk)
	// Note: a s.Gotalk.OnAccept handler is installed in prepareToServe

	// XXX DEBUG some test handlers
	s.GotalkRoute("ping", func(c *gotalk.Sock) (string, error) {
		return "pong", nil
	})
	s.GotalkRoute("test/message", func(c *gotalk.Sock, message string) (string, error) {
		return "pong: " + message, nil
	})

	return s
}

// -----------------------------------------------------------------------------------------------
// routes

// Route registers a HTTP request handler for the given pattern.
//
// Patterns name fixed, rooted paths, like "/favicon.ico", or rooted subtrees, like
// "/images/" (note the trailing slash). Longer patterns take precedence over
// shorter ones, so that if there are handlers registered for both "/images/" and
// "/images/thumbnails/", the latter handler will be called for paths beginning
// "/images/thumbnails/" and the former will receive requests for any other paths
// in the "/images/" subtree.
//
// Note that since a pattern ending in a slash names a rooted subtree, the pattern
// "/" matches all paths not matched by other registered patterns, not just the URL
// with Path == "/".
//
// If a subtree has been registered and a request is received naming the subtree
// root without its trailing slash, ServeMux redirects that request to the subtree
// root (adding the trailing slash). This behavior can be overridden with a
// separate registration for the path without the trailing slash. For example,
// registering "/images/" causes ServeMux to redirect a request for "/images" to
// "/images/", unless "/images" has been registered separately.
//
// Patterns may optionally begin with a host name, restricting matches to URLs on
// that host only. Host-specific patterns take precedence over general patterns, so
// that a handler might register for the two patterns "/codesearch" and
// "codesearch.google.com/" without also taking over requests for
// "http://www.google.com/".
//
// ServeMux also takes care of sanitizing the URL request path and the Host header,
// stripping the port number and redirecting any request containing . or ..
// elements or repeated slashes to an equivalent, cleaner URL.
//
func (s *Server) Route(pattern string, handler func(*Transaction)) {
	s.Routes.HandleFunc(pattern, s.createHttpRouteHandler(handler))
}

// GotalkRoute registers a Gotalk request handler for the given operation,
// with automatic JSON encoding of values.
//
// `handler` must conform to one of the following signatures:
//   func(*WebSocket, string, interface{}) (interface{}, error) ; takes socket, op and parameters
//   func(*WebSocket, interface{}) (interface{}, error)         ; takes socket and parameters
//   func(*WebSocket) (interface{}, error)                      ; takes socket only
//   func(interface{}) (interface{}, error)                     ; takes parameters, but no socket
//   func() (interface{},error)                                 ; takes no socket or parameters
//
// Optionally the `interface{}` return value can be omitted, i.e:
//   func(*WebSocket, string, interface{}) error
//   func(*WebSocket, interface{}) error
//   func(*WebSocket) error
//   func(interface{}) error
//   func() error
//
// If `op` is empty, handle all requests which doesn't have a specific handler registered.
func (s *Server) GotalkRoute(op string, handler interface{}) {
	s.Gotalk.Handlers.Handle(op, handler)
}

func (s *Server) createHttpRouteHandler(f func(*Transaction)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := NewTransaction(s, w, r)

		// recover panic and turn it into an error
		defer func() {
			if err := recover(); err != nil {
				var tail string
				if DevMode {
					tail = "\n" + string(debug.Stack())
				}
				s.loge("error in http handler: %v%s", err, tail)
				t.RespondWithMessage(500, err)
			}
		}()

		f(t)
	}
}

// -----------------------------------------------------------------------------------------------

func (s *Server) prepareToServe() {
	// Configure logger
	if s.Logger == nil {
		s.Logger = log.RootLogger
	}
	if s.Server.ErrorLog == nil {
		// From go's net/http documentation on Server.ErrorLog:
		//   ErrorLog specifies an optional logger for errors accepting connections,
		//   unexpected behavior from handlers, and underlying FileSystem errors.
		//   If nil, logging is done via the log package's standard logger.
		s.Server.ErrorLog = s.Logger.GoLogger(log.LevelError)
	}

	// Unless there's already a handler registered for "/", install a "catch all" file handler.
	// s.fileHandler is nil if PubDir is empty.
	if s.fileHandler != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					// ignore error "http: multiple registrations for /"
				}
			}()
			s.Routes.Handle("/", s.fileHandler)
		}()
	}

	// Install the gotalk connect handler here rather than when creating the Server struct so that
	// in case the user installed a handler, we can wrap it.
	s.gotalkOnConnectUser = s.Gotalk.OnConnect // save any user handler
	s.Gotalk.OnConnect = s.gotalkOnConnect
}

func (s *Server) returnFromServe(err error) error {
	// Restore previously replaced Gotalk.OnConnect
	s.Gotalk.OnConnect = s.gotalkOnConnectUser
	s.gotalkOnConnectUser = nil

	if err == http.ErrServerClosed {
		// returned from Serve functions when server.Shutdown() was initiated
		err = nil
	}
	return err
}

func (s *Server) gotalkOnConnect(sock *gotalk.WebSocket) {
	s.logd("gotalk sock#%p connected", sock)

	// call user handler
	if s.gotalkOnConnectUser != nil {
		s.gotalkOnConnectUser(sock)
		if sock.IsClosed() {
			return
		}
	}

	// register close handler
	userCloseHandler := sock.CloseHandler
	sock.CloseHandler = func(sock *gotalk.WebSocket, closeCode int) {
		s.logd("gotalk sock#%p disconnected", sock)
		s.gotalkSocksMu.Lock()
		defer s.gotalkSocksMu.Unlock()
		delete(s.gotalkSocks, sock)
		sock.CloseHandler = nil
		if userCloseHandler != nil {
			userCloseHandler(sock, closeCode)
		}
	}

	// register connection
	s.gotalkSocksMu.Lock()
	if s.gotalkSocks == nil {
		s.gotalkSocks = make(map[*gotalk.WebSocket]int)
	}
	s.gotalkSocks[sock] = 1
	s.gotalkSocksMu.Unlock()
}

func (s *Server) Serve(l net.Listener) error {
	s.prepareToServe()
	return s.Server.Serve(l)
}

func (s *Server) Addr() string {
	return s.Server.Addr
}

func (s *Server) Close() error {
	return s.Server.Close()
}

func (s *Server) Shutdown(ctx context.Context, stoppedAcceptingCallback func()) error {
	if stoppedAcceptingCallback != nil {
		s.Server.RegisterOnShutdown(stoppedAcceptingCallback)
	}
	s.Server.SetKeepAlivesEnabled(false)
	return s.Server.Shutdown(ctx)
}

//
// —————————————————————————————————————————————————————————————————————————————————————————————
// logging
//

func (s *Server) loge(format string, v ...interface{}) {
	s.Logger.Error(format, v...)
}
func (s *Server) logw(format string, v ...interface{}) {
	s.Logger.Warn(format, v...)
}
func (s *Server) logi(format string, v ...interface{}) {
	s.Logger.Info(format, v...)
}
func (s *Server) logd(format string, v ...interface{}) {
	s.Logger.LogDebug(1, format, v...)
}

// -----------------------------------------------------------------------------------------------
// Graceful shutdown

var (
	// protects the following fields
	gracefulShutdownMu sync.Mutex

	// listening servers which opted in to graceful shutdown
	gracefulShutdownServers []*Server

	// channel that closes when all servers has completed shutdown
	gracefulShutdownChan chan struct{}
)

// EnableGracefulShutdown enables the server to be shut down gracefully, allowing active
// connections to end within shutdownTimeout.
//
// When graceful shutdown is enabled, SIGINT and SIGTERM signals to the process initiates
// shutdown of all servers which opted in to graceful shutdown. Servers which didn't will close
// as usual (immediately.)
//
// See net/http.Server.Shutdown for details on shutdown semantics.
//
func (s *Server) EnableGracefulShutdown(shutdownTimeout time.Duration) chan struct{} {
	if shutdownTimeout == 0 {
		panic("timeout can not be 0")
	}
	s.gracefulShutdownTimeout = shutdownTimeout
	gracefulShutdownMu.Lock()
	defer gracefulShutdownMu.Unlock()
	gracefulShutdownServers = append(gracefulShutdownServers, s)
	if gracefulShutdownChan == nil {
		// Install signal handler
		gracefulShutdownChan = make(chan struct{})
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-quit
			gracefulShutdownAll()
		}()
	}
	return gracefulShutdownChan
}

func (s *Server) DisableGracefulShutdown() {
	s.gracefulShutdownTimeout = 0
	// Note: It may seem like a good idea to remove s from gracefulShutdownServers but that
	// would not work. If the caller is waiting for gracefulShutdownChan to close, after Listen
	// returns, Listen would never return since the server would never be shut down.
}

func gracefulShutdownAll() {
	gracefulShutdownMu.Lock()
	defer gracefulShutdownMu.Unlock()

	var wg sync.WaitGroup

	shutdownServer := func(server *Server) {
		defer wg.Done()
		if server.gracefulShutdownTimeout == 0 {
			// DisableGracefulShutdown was called; close server to make sure that the caller's
			// Listen call ends & returns.
			server.Server.Close()
			return
		}
		server.Logger.Debug("graceful shutdown initiated")
		ctx, cancel := context.WithTimeout(context.Background(), server.gracefulShutdownTimeout)
		defer cancel()
		server.Server.SetKeepAlivesEnabled(false)
		if err := server.Server.Shutdown(ctx); err != nil {
			server.Logger.Error("graceful shutdown error: %s", err)
		} else {
			server.Logger.Debug("graceful shutdown complete")
		}
	}

	for i, server := range gracefulShutdownServers {
		wg.Add(1)
		if i == len(gracefulShutdownServers)-1 {
			shutdownServer(server)
		} else {
			go shutdownServer(server)
		}
	}

	wg.Wait()
	close(gracefulShutdownChan)
	gracefulShutdownChan = nil
	gracefulShutdownServers = nil
}

//
//
// ----------------------------------------
//
//

// gotalk.Handle("users.list", func(c *gotalk.Sock) ([]*User, error) {
//   keyPrefix := []byte("user:")
//   it := s.db.NewIterator(util.BytesPrefix(keyPrefix), nil)
//   defer it.Release()
//   var users []*User
//   for it.Next() {
//     // logf("username: %s", string(it.Key()[len(keyPrefix):]))
//     u := decodeUser(it.Value())
//     if u != nil {
//       users = append(users, u)
//     }
//   }
//   return users, it.Error()
// })

// gotalk.Handle("profile.save", func(c *gotalk.Sock, info User) error {
//   u := s.userLoadFromGotalk(c)
//   u.AboutURL = info.AboutURL
//   u.AboutText = info.AboutText
//   err := u.store(s.db)
//   if err != nil {
//     logf("[profile.save] failed to store user: %s", err.Error())
//   } else {
//     s.Broadcast("user-change", u)
//   }
//   return err
// })

/*func (s *Server) gotalkOnConnect(sock *gotalk.Sock) {
  ws, ok := sock.Conn().(*websocket.Conn)
  if !ok {
    err := fmt.Errorf("gotalk socket not connected over websocket")
    logf("[gotalk#%p] error %v", sock, err)
    sock.Notify("error", fmt.Sprintf("%v", err))
    sock.Close()
    return
  }

  se := s.sessions.GetHTTP(ws.Request())
  user := userLoadFromSession(ws.Request().Context(), s.db, se)

  if user == nil {
    // not signed in
    logf("[gotalk#%p] open. user=anonymous", sock)
    if DEV_BUILD {
      sock.CloseHandler = func(sock *gotalk.Sock, _ int) {
        logf("[gotalk#%p] close", sock)
      }
    }
  } else {
    logf("[gotalk#%p] open. user=#%d", sock, user.Id)

    // Register connection
    s.socksmu.Lock()
    s.socks[sock] = 1
    s.socksmu.Unlock()

    // Unregister when connection closes
    sock.CloseHandler = func(sock *gotalk.Sock, _ int) {
      logf("[gotalk#%p] close", sock)
      s.socksmu.Lock()
      defer s.socksmu.Unlock()
      delete(s.socks, sock)
    }

    sock.UserData = user.Id
  }

  // notifcy client about the current viewer (even if its nil, so the client knows)
  sock.Notify("viewer", user)
}*/
