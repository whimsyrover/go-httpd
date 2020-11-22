package httpd

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rsms/go-httpd/session"
	"github.com/rsms/go-log"
	"github.com/rsms/gotalk"
)

type Server struct {
	Logger   *log.Logger   // defaults to log.RootLogger
	PubDir   string        // directory to serve files from. File serving is disabled if empty.
	Routes   Router        // http request routes
	Server   http.Server   // underlying http server
	Sessions session.Store // Call Sessions.SetStorage(s) to enable sessions

	Gotalk     *gotalk.WebSocketServer // set to nil to disable gotalk
	GotalkPath string                  // defaults to "/gotalk/"

	fileHandler http.Handler // serves pubdir (nil if len(PubDir)==0)

	gotalkSocksMu       sync.RWMutex                 // protects gotalkSocks field
	gotalkSocks         map[*gotalk.WebSocket]int    // currently connected gotalk sockets
	gotalkOnConnectUser func(sock *gotalk.WebSocket) // saved value of .Gotalk.OnConnect

	gracefulShutdownTimeout time.Duration
}

func NewServer(pubDir, addr string) *Server {
	s := &Server{
		PubDir: pubDir,
		Logger: log.RootLogger,
		Server: http.Server{
			Addr:           addr,
			WriteTimeout:   10 * time.Second,
			ReadTimeout:    10 * time.Second,
			MaxHeaderBytes: 1 << 20, // 1MB
		},
		Gotalk:     gotalk.WebSocketHandler(),
		GotalkPath: "/gotalk/",
	}

	if len(pubDir) > 0 {
		s.fileHandler = http.FileServer(http.Dir(pubDir))
	}

	s.Server.Handler = s
	s.Gotalk.Handlers = gotalk.NewHandlers()
	s.Server.RegisterOnShutdown(func() {
		// close all connected sockets
		s.gotalkSocksMu.RLock()
		defer s.gotalkSocksMu.RUnlock()
		for s := range s.gotalkSocks {
			s.Close()
		}
	})

	return s
}

// ServeHTTP serves a HTTP request using this server
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.RequestURI == "*" {
		if r.ProtoAtLeast(1, 1) {
			w.Header().Set("Connection", "close")
		}
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// CONNECT requests are not canonicalized
	if r.Method != "CONNECT" {
		// strip port and clean path
		url := *r.URL
		path := cleanPath(r.URL.Path)

		// redirect if the path was not canonical
		if path != r.URL.Path {
			url.Path = path
			http.Redirect(w, r, url.String(), http.StatusMovedPermanently)
			return
		}

		// set cleaned valued
		url.Path = path
		r.Host = stripHostPort(r.Host)
	}

	// gotalk?
	if s.Gotalk != nil && s.GotalkPath != "" && strings.HasPrefix(r.URL.Path, s.GotalkPath) {
		// Note: s.Gotalk.OnAccept handler is installed in prepareToServe
		s.Gotalk.ServeHTTP(w, r)
		return
	}

	// create a new transaction
	t := NewTransaction(s, w, r)

	// recover panic and turn it into an error
	defer func() {
		if err := recover(); err != nil {
			s.LogError("ServeHTTP error: %v", err)
			if s.Logger.Level <= log.LevelDebug {
				s.LogDebug("ServeHTTP error: %s\n%s", err, string(debug.Stack()))
			}
			t.RespondWithMessage(500, err)
		}
	}()

	// serve
	if s.Routes.MaybeServeHTTP(t) {
		return
	}

	// fallback to serving files, if configured
	if s.fileHandler != nil {
		s.fileHandler.ServeHTTP(w, r)
		return
	}

	// 404 not found
	t.RespondWithStatusNotFound()
}

// cleanPath returns the canonical path for p, eliminating . and .. elements.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	np := path.Clean(p)
	// path.Clean removes trailing slash except for root;
	// put the trailing slash back if necessary.
	if p[len(p)-1] == '/' && np != "/" {
		// Fast path for common case of p being the string we want:
		if len(p) == len(np)+1 && strings.HasPrefix(p, np) {
			np = p
		} else {
			np += "/"
		}
	}
	return np
}

// stripHostPort returns h without any trailing ":<port>".
func stripHostPort(h string) string {
	// If no port on host, return unchanged
	if strings.IndexByte(h, ':') == -1 {
		return h
	}
	host, _, err := net.SplitHostPort(h)
	if err != nil {
		return h // on error, return unchanged
	}
	return host
}

// -----------------------------------------------------------------------------------------------
// routes

// Handler responds to a HTTP request
type Handler interface {
	ServeHTTP(*Transaction)
}

// Handle registers a HTTP request handler for the given pattern.
//
// The server takes care of sanitizing the URL request path and the Host header,
// stripping the port number and redirecting any request containing . or ..
// elements or repeated slashes to an equivalent, cleaner URL.
func (s *Server) Handle(pattern string, handler Handler) {
	s.Routes.Handle(pattern, handler)
}

// HandleFunc registers a HTTP request handler function for the given pattern.
//
// The server takes care of sanitizing the URL request path and the Host header,
// stripping the port number and redirecting any request containing . or ..
// elements or repeated slashes to an equivalent, cleaner URL.
func (s *Server) HandleFunc(pattern string, handler func(*Transaction)) {
	s.Routes.HandleFunc(pattern, handler)
}

// HandleGotalk registers a Gotalk request handler for the given operation,
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
func (s *Server) HandleGotalk(op string, handler interface{}) {
	s.Gotalk.Handlers.Handle(op, handler)
}

// -----------------------------------------------------------------------------------------------

// protoname should be "http" or "https"
func (s *Server) bindListener(protoname string) (net.Listener, error) {
	addr := s.Server.Addr
	if addr == "" {
		addr = ":" + protoname
	}
	return net.Listen("tcp", addr)
}

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

	// // Unless there's already a handler registered for "/", install a "catch all" file handler.
	// // s.fileHandler is nil if PubDir is empty.
	// if s.fileHandler != nil {
	// 	func() {
	// 		defer func() {
	// 			if r := recover(); r != nil {
	// 				// ignore error "http: multiple registrations for /"
	// 			}
	// 		}()
	// 		s.Routes.Handle("/", s.fileHandler)
	// 	}()
	// }

	if s.Gotalk != nil {
		// Install the gotalk connect handler here rather than when creating the Server struct so that
		// in case the user installed a handler, we can wrap it.
		s.gotalkOnConnectUser = s.Gotalk.OnConnect // save any user handler
		s.Gotalk.OnConnect = s.gotalkOnConnect
	}
}

func (s *Server) justBeforeServing(ln net.Listener, protoname, extraLogMsg string) {
	s.LogInfo("listening on %s://%s (pubdir %q%s)", protoname, ln.Addr(), s.PubDir, extraLogMsg)
}

func (s *Server) returnFromServe(err error) error {
	if s.Gotalk != nil {
		// Restore previously replaced Gotalk.OnConnect
		s.Gotalk.OnConnect = s.gotalkOnConnectUser
		s.gotalkOnConnectUser = nil
	}

	if err == http.ErrServerClosed {
		// returned from Serve functions when server.Shutdown() was initiated
		err = nil
	}
	return err
}

func (s *Server) gotalkOnConnect(sock *gotalk.WebSocket) {
	s.LogDebug("gotalk sock#%p connected", sock)

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
		s.LogDebug("gotalk sock#%p disconnected", sock)
		s.gotalkSocksMu.Lock()
		delete(s.gotalkSocks, sock)
		s.gotalkSocksMu.Unlock()
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

// RangeGotalkSockets calls f with each currently-connected gotalk socket.
// Gotalk socket acceptance will be blocked while this method is called as the underlying
// socket list is locked during the call.
// If f returns false then iteration stops early.
func (s *Server) RangeGotalkSockets(f func(*gotalk.WebSocket)bool) {
	s.gotalkSocksMu.RLock()
	defer s.gotalkSocksMu.RUnlock()
	for s := range s.gotalkSocks {
		if !f(s) {
			break
		}
	}
}

func (s *Server) Serve(ln net.Listener) error {
	s.prepareToServe()
	s.justBeforeServing(ln, "http", "")
	return s.Server.Serve(ln)
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

func (s *Server) LogError(format string, v ...interface{}) {
	s.Logger.Error(format, v...)
}
func (s *Server) LogWarn(format string, v ...interface{}) {
	s.Logger.Warn(format, v...)
}
func (s *Server) LogInfo(format string, v ...interface{}) {
	s.Logger.Info(format, v...)
}
func (s *Server) LogDebug(format string, v ...interface{}) {
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
