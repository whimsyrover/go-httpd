package httpd

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/rsms/go-httpd/session"
	"github.com/rsms/go-httpd/util"
)

// Transaction represents a HTTP request + response.
// Implements io.Writable
// Implements http.ResponseWriter
//
type Transaction struct {
	http.ResponseWriter
	Request *http.Request
	Server  *Server
	URL     *url.URL
	Status  int                    // response status code (200 by default)
	AuxData map[string]interface{} // can be used to associate arbitrary data with a transaction

	headerWritten bool
	query         url.Values // initially nil (it's a map); cached value of .URL.Query()
	session       *session.Session
}

// thread-safe pool of free Transaction objects reduces memory thrash
var httpTransactionFreePool = sync.Pool{}

func init() {
	// must set in init function since the New function references gcTransaction
	// which depends on httpTransactionFreePool.
	httpTransactionFreePool.New = func() interface{} {
		// called when there are no free items in the pool
		return new(Transaction)
	}
}

func gcTransaction(t *Transaction) {
	// called by the garbage collector when t is definitely garbage.
	// instead of letting t be collected, we put it into our free list.
	//
	// Clear references to data to allow that data to be garbage collected.
	t.ResponseWriter = nil
	t.Request = nil
	t.URL = nil
	t.headerWritten = false
	t.query = nil
	// t.user = nil
	// t.userLoaded = false
	t.session = nil
	httpTransactionFreePool.Put(t)
}

func NewTransaction(server *Server, w http.ResponseWriter, r *http.Request) *Transaction {
	t := httpTransactionFreePool.Get().(*Transaction)
	runtime.SetFinalizer(t, gcTransaction)
	t.ResponseWriter = w
	t.Request = r
	t.Server = server
	t.URL = r.URL
	t.Status = 200
	return t
}

// // parameter from URL route
// func (t *Transaction) RouteVar(name string) string {
// 	return mux.Vars(t.Request)[name]
// }

// parameter from URL query string
func (t *Transaction) QueryVar(name string) string {
	return t.Query().Get(name)
}

// parameter from POST form data
func (t *Transaction) FormVar(name string) string {
	return t.Request.PostFormValue(name)
}

// parameter from current session (nil if not found or no session)
func (t *Transaction) SessionVar(name string) interface{} {
	return t.Session().Get(name)
}

// URL query string parameters
func (t *Transaction) Query() url.Values {
	if t.query == nil {
		t.query = t.URL.Query()
	}
	return t.query
}

// --------------------------------------------------------------------------------------
// Responding

// SetLastModified sets Last-Modified header if modtime != 0
func (t *Transaction) SetLastModified(modtime time.Time) {
	if !isZeroTime(modtime) {
		t.Header().Set("Last-Modified", modtime.UTC().Format(http.TimeFormat))
	}
}

func (t *Transaction) SetNoCacheHeaders() {
	h := t.Header()
	h.Set("Cache-Control", "no-cache, no-store, must-revalidate, pre-check=0, post-check=0")
	h.Set("Pragma", "no-cache")
	h.Set("Strict-Transport-Security", "max-age=15552000; preload") // for HTTPS
}

// SetCookie sets or adds a cookie to the response header.
// See HeaderSetCookie for more details.
func (t *Transaction) SetCookie(cookie string) error {
	return util.HeaderSetCookie(t.Header(), cookie)
}

func (t *Transaction) WriteHeader(statusCode int) {
	if !t.headerWritten {
		t.headerWritten = true
		if t.session != nil {
			if err := t.session.SaveHTTP(t); err != nil {
				t.Server.loge("Transaction.WriteHeader,Session.SaveHTTP error: %v", err)
			}
		}
		t.ResponseWriter.WriteHeader(statusCode)
	}
}

func (t *Transaction) Write(data []byte) (int, error) {
	t.WriteHeader(t.Status)
	return t.ResponseWriter.Write(data)
}

func (t *Transaction) WriteString(s string) (int, error) {
	return t.Write([]byte(s))
}

func (t *Transaction) Print(a interface{}) (int, error) {
	return fmt.Fprint(t, a)
}

func (t *Transaction) Printf(format string, arg ...interface{}) (int, error) {
	return fmt.Fprintf(t, format, arg...)
}

func (t *Transaction) Flush() bool {
	t.WriteHeader(t.Status)
	flusher, ok := t.ResponseWriter.(http.Flusher)
	if ok {
		flusher.Flush()
	}
	return ok
}

func (t *Transaction) WriteTemplate(tpl Template, data interface{}) error {
	buf, err := tpl.ExecBuf(data)
	if err != nil {
		return err
	}
	t.Header().Set("Content-Type", "text/html; charset=utf-8")
	t.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	t.Write(buf)
	return nil
}

func (t *Transaction) WriteHtmlTemplateFile(filename string, data interface{}) {
	filename = t.AbsFilePath(filename)
	// TODO: cache
	tpl, err := ParseHtmlTemplateFile(filename)
	if err == nil {
		err = t.WriteTemplate(tpl, data)
	}
	if err != nil {
		panic(err)
	}
}

func (t *Transaction) WriteHtmlTemplateStr(templateSource string, data interface{}) {
	tpl, err := ParseHtmlTemplate("main", templateSource)
	if err == nil {
		err = t.WriteTemplate(tpl, data)
	}
	if err != nil {
		panic(err)
	}
}

// AbsFilePath takes a relative or absolute filename and returns an absolute filename.
// If the filename is relative it will be resolved to t.Server.PubDir (if PubDir is empty,
// filename is resolve to current working directory.) An absolute filename is returned verbatim.
func (t *Transaction) AbsFilePath(filename string) string {
	if filepath.IsAbs(filename) {
		return filename
	}
	if len(t.Server.PubDir) != 0 {
		return filepath.Join(t.Server.PubDir, filename)
	}
	filename, err := filepath.Abs(filename)
	if err != nil {
		panic(err)
	}
	return filename
}

func (t *Transaction) ServeFile(filename string) {
	filename = t.AbsFilePath(filename)
	http.ServeFile(t, t.Request, filename)
}

func (t *Transaction) RespondWithMessage(statusCode int, msg interface{}) {
	body := "<body><h1>" + http.StatusText(statusCode) + "</h1><code>" +
		html.EscapeString(fmt.Sprintf("%v", msg)) + "</code></body>"
	t.Header().Set("Content-Type", "text/html; charset=utf-8")
	t.Header().Set("Content-Length", strconv.Itoa(len(body)))
	t.Status = statusCode
	t.Write([]byte(body))
}

// Redirect sends a redirection response (HTTP 302 "Found")
func (t *Transaction) Redirect(url string) {
	http.Redirect(t, t.Request, url, http.StatusFound)
}

// RedirectToReferrer redirects the request to the referrer or if no referrer is set, or the
// referrer is the current URL, redirects to fallbackUrl.
func (t *Transaction) RedirectToReferrer(fallbackUrl string) {
	referrer := t.DifferentReferrerURL()
	if referrer != nil {
		t.Redirect(referrer.String())
	} else {
		t.Redirect(fallbackUrl)
	}
}

// DifferentReferrer returns a URL of the "referer" request field if present.
// If the referrer's path is the same as t.URL.Path then nil is returned.
func (t *Transaction) DifferentReferrerURL() *url.URL {
	referrer := t.Request.Header.Get("referer") // yup, it's misspelled
	if referrer != "" {
		refurl, err := url.Parse(referrer)
		if err == nil && refurl.Path != t.URL.Path {
			return refurl
		}
	}
	return nil
}

// -------------------------------------------------------------------------------------------
// Context

func (t *Transaction) Context() context.Context {
	return t.Request.Context()
}

func (t *Transaction) ContextWithTimeout(
	timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(t.Request.Context(), timeout)
}

// -------------------------------------------------------------------------------------------
// Session

// Session returns the session for the transaction.
// If the server has a valid session store, this always returns a valid Session object, which
// may be empty in case there's no session.
// Returns nil only when the server does not have a valid session store.
//
func (t *Transaction) Session() *session.Session {
	if t.session == nil {
		// Note: LoadHTTP always returns a valid Session object
		t.session, _ = t.Server.Sessions.LoadHTTP(t.Request) // ignore error
	}
	return t.session
}

func (t *Transaction) SaveSession() {
	if t.session != nil {
		if err := t.session.SaveHTTP(t); err != nil {
			t.Server.Logger.Warn("Transaction.SaveSession: %v", err)
		}
	}
}

func (t *Transaction) ClearSession() {
	s := t.Session()
	if s != nil {
		s.Clear()
		if err := s.SaveHTTP(t); err != nil {
			t.Server.Logger.Warn("Transaction.ClearSession: %v", err)
		}
	}
}

// // RequireUser verifies that the request is from an authenticated user.
// //
// // If the user is authenticated, their corresponding User object is returned.
// // In this case, the caller should complete the response.
// //
// // If the user is not authenticated, or their session has become invalid, a redirect
// // response is sent to the sign-in page and nil is returned.
// // In the case of nil being returned, the caller should NOT modify the response.
// //
// func (t *Transaction) RequireUser1() *User1 {
// 	setNoCacheHeaders(t)
// 	user := t.User1()
// 	if user == nil {
// 		// logf("unathuenticated request; redirecting to sign-in")
// 		redirectToLogIn(t)
// 		return nil
// 	}
// 	return user
// }
