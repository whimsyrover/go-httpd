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

	"github.com/rsms/go-httpd/route"
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
	routeMatch    *route.Match // non-nil when the transaction went through HttpRouter
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

// Method returns the HTTP request method (i.e. GET, POST, etc.)
func (t *Transaction) Method() string { return t.Request.Method }

// Var returns the first value for the named component of the query.
//
// Search order:
//   1. URL route parameter (e.g. "id" in "/user/{id}")
//   2. FORM or PUT parameters
//   3. URL query-string parameters
//
// This function calls Request.ParseMultipartForm and Request.ParseForm if necessary and ignores
// any errors returned by these functions. If key is not present, Var returns the empty string.
//
// To access multiple values of the same key, call Request.ParseForm and then inspect
// Request.Form directly.
func (t *Transaction) Var(name string) string {
	if t.routeMatch != nil {
		if value := t.routeMatch.Var(name); value != "" {
			return value
		}
	}
	return t.Request.FormValue(name)
}

// parameter from URL route
func (t *Transaction) RouteVar(name string) string {
	if t.routeMatch == nil {
		return ""
	}
	return t.routeMatch.Var(name)
}

// FormVar retrieves a POST, PATCH or PUT form parameter
func (t *Transaction) FormVar(name string) string {
	return t.Request.PostFormValue(name)
}

// QueryVar retrieves a URL query-string parameter
func (t *Transaction) QueryVar(name string) string {
	return t.Query().Get(name)
}

// SessionVar retrieves a session parameter (nil if not found or no session)
func (t *Transaction) SessionVar(name string) interface{} {
	return t.Session().Get(name)
}

// Query returns all URL query-string parameters
func (t *Transaction) Query() url.Values {
	if t.query == nil {
		t.query = t.URL.Query()
	}
	return t.query
}

// Form returns all POST, PATCH or PUT parameters
func (t *Transaction) Form() url.Values {
	// cause ParseMultipartForm to be called with
	const maxMemory = 32 << 20 // 32 MB (matches defaultMaxMemory of go/net/http/request.go)
	if t.Request.PostForm == nil {
		t.Request.ParseMultipartForm(maxMemory)
	}
	// ignore error (complains if the conent type is not multipart)
	return t.Request.PostForm
}

func (t *Transaction) AuxVar(name string) interface{} {
	return t.AuxData[name]
}

func (t *Transaction) SetAuxVar(name string, value interface{}) {
	if t.AuxData == nil {
		t.AuxData = make(map[string]interface{})
	}
	t.AuxData[name] = value
}

// RoutePath returns the request URL path relative to the Router that dispatched this request.
// If the dispatch was done by t.Server.Router then this is identical to t.URL.Path.
func (t *Transaction) RoutePath() string {
	if t.routeMatch != nil {
		return t.routeMatch.Path
	}
	return t.URL.Path
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
				t.Server.LogError("Transaction.WriteHeader;Session.SaveHTTP error: %v", err)
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
	body := "<body><h1>" + http.StatusText(statusCode) + "</h1>"
	if msg != nil {
		body += "<code>" + html.EscapeString(fmt.Sprint(msg)) + "</code>"
	}
	body += "</body>"
	t.Header().Set("Content-Type", "text/html; charset=utf-8")
	t.Header().Set("Content-Length", strconv.Itoa(len(body)))
	t.Status = statusCode
	t.Write([]byte(body))
}

func (t *Transaction) RespondWithStatus(statusCode int) {
	t.RespondWithMessage(statusCode, t.URL.String())
}

func (t *Transaction) rws(statusCode int) { t.RespondWithStatus(statusCode) }

func (t *Transaction) RespondWithStatusContinue()           { t.rws(100) }
func (t *Transaction) RespondWithStatusSwitchingProtocols() { t.rws(101) }
func (t *Transaction) RespondWithStatusProcessing()         { t.rws(102) }
func (t *Transaction) RespondWithStatusEarlyHints()         { t.rws(103) }

func (t *Transaction) RespondWithStatusOK()                   { t.rws(200) }
func (t *Transaction) RespondWithStatusCreated()              { t.rws(201) }
func (t *Transaction) RespondWithStatusAccepted()             { t.rws(202) }
func (t *Transaction) RespondWithStatusNonAuthoritativeInfo() { t.rws(203) }
func (t *Transaction) RespondWithStatusNoContent()            { t.rws(204) }
func (t *Transaction) RespondWithStatusResetContent()         { t.rws(205) }
func (t *Transaction) RespondWithStatusPartialContent()       { t.rws(206) }
func (t *Transaction) RespondWithStatusMultiStatus()          { t.rws(207) }
func (t *Transaction) RespondWithStatusAlreadyReported()      { t.rws(208) }
func (t *Transaction) RespondWithStatusIMUsed()               { t.rws(226) }

func (t *Transaction) RespondWithStatusMultipleChoices()  { t.rws(300) }
func (t *Transaction) RespondWithStatusMovedPermanently() { t.rws(301) }
func (t *Transaction) RespondWithStatusFound()            { t.rws(302) }
func (t *Transaction) RespondWithStatusSeeOther()         { t.rws(303) }
func (t *Transaction) RespondWithStatusNotModified()      { t.rws(304) }
func (t *Transaction) RespondWithStatusUseProxy()         { t.rws(305) }

func (t *Transaction) RespondWithStatusTemporaryRedirect() { t.rws(307) }
func (t *Transaction) RespondWithStatusPermanentRedirect() { t.rws(308) }

func (t *Transaction) RespondWithStatusBadRequest()                   { t.rws(400) }
func (t *Transaction) RespondWithStatusUnauthorized()                 { t.rws(401) }
func (t *Transaction) RespondWithStatusPaymentRequired()              { t.rws(402) }
func (t *Transaction) RespondWithStatusForbidden()                    { t.rws(403) }
func (t *Transaction) RespondWithStatusNotFound()                     { t.rws(404) }
func (t *Transaction) RespondWithStatusMethodNotAllowed()             { t.rws(405) }
func (t *Transaction) RespondWithStatusNotAcceptable()                { t.rws(406) }
func (t *Transaction) RespondWithStatusProxyAuthRequired()            { t.rws(407) }
func (t *Transaction) RespondWithStatusRequestTimeout()               { t.rws(408) }
func (t *Transaction) RespondWithStatusConflict()                     { t.rws(409) }
func (t *Transaction) RespondWithStatusGone()                         { t.rws(410) }
func (t *Transaction) RespondWithStatusLengthRequired()               { t.rws(411) }
func (t *Transaction) RespondWithStatusPreconditionFailed()           { t.rws(412) }
func (t *Transaction) RespondWithStatusRequestEntityTooLarge()        { t.rws(413) }
func (t *Transaction) RespondWithStatusRequestURITooLong()            { t.rws(414) }
func (t *Transaction) RespondWithStatusUnsupportedMediaType()         { t.rws(415) }
func (t *Transaction) RespondWithStatusRequestedRangeNotSatisfiable() { t.rws(416) }
func (t *Transaction) RespondWithStatusExpectationFailed()            { t.rws(417) }
func (t *Transaction) RespondWithStatusTeapot()                       { t.rws(418) }
func (t *Transaction) RespondWithStatusMisdirectedRequest()           { t.rws(421) }
func (t *Transaction) RespondWithStatusUnprocessableEntity()          { t.rws(422) }
func (t *Transaction) RespondWithStatusLocked()                       { t.rws(423) }
func (t *Transaction) RespondWithStatusFailedDependency()             { t.rws(424) }
func (t *Transaction) RespondWithStatusTooEarly()                     { t.rws(425) }
func (t *Transaction) RespondWithStatusUpgradeRequired()              { t.rws(426) }
func (t *Transaction) RespondWithStatusPreconditionRequired()         { t.rws(428) }
func (t *Transaction) RespondWithStatusTooManyRequests()              { t.rws(429) }
func (t *Transaction) RespondWithStatusRequestHeaderFieldsTooLarge()  { t.rws(431) }
func (t *Transaction) RespondWithStatusUnavailableForLegalReasons()   { t.rws(451) }

func (t *Transaction) RespondWithStatusInternalServerError()           { t.rws(500) }
func (t *Transaction) RespondWithStatusNotImplemented()                { t.rws(501) }
func (t *Transaction) RespondWithStatusBadGateway()                    { t.rws(502) }
func (t *Transaction) RespondWithStatusServiceUnavailable()            { t.rws(503) }
func (t *Transaction) RespondWithStatusGatewayTimeout()                { t.rws(504) }
func (t *Transaction) RespondWithStatusHTTPVersionNotSupported()       { t.rws(505) }
func (t *Transaction) RespondWithStatusVariantAlsoNegotiates()         { t.rws(506) }
func (t *Transaction) RespondWithStatusInsufficientStorage()           { t.rws(507) }
func (t *Transaction) RespondWithStatusLoopDetected()                  { t.rws(508) }
func (t *Transaction) RespondWithStatusNotExtended()                   { t.rws(510) }
func (t *Transaction) RespondWithStatusNetworkAuthenticationRequired() { t.rws(511) }

// Redirect sends a redirection response by setting the "location" header field.
// The url may be a path relative to the request path.
//
// If the Content-Type header has not been set, Redirect sets it to "text/html; charset=utf-8"
// and writes a small HTML body. Setting the Content-Type header to any value, including nil,
// disables that behavior.
func (t *Transaction) Redirect(url string, code int) {
	http.Redirect(t, t.Request, url, code)
}

// TemporaryRedirect sends a redirection response HTTP 302.
// The user agent may change method (usually it uses GET) but it's ambiguous.
func (t *Transaction) TemporaryRedirect(url string) {
	t.Redirect(url, http.StatusFound)
}

// TemporaryRedirectGET sends a redirection response HTTP 303.
// The new location will be requested using the GET method.
func (t *Transaction) TemporaryRedirectGET(url string) {
	code := http.StatusFound
	if t.Request.ProtoAtLeast(1, 1) {
		code = http.StatusSeeOther
	}
	t.Redirect(url, code)
}

// TemporaryRedirectSameMethod sends a redirection response HTTP 307.
// The new location will be requested using the same method as the current request.
func (t *Transaction) TemporaryRedirectSameMethod(url string) {
	code := http.StatusFound
	if t.Request.ProtoAtLeast(1, 1) {
		code = http.StatusTemporaryRedirect
	}
	t.Redirect(url, code)
}

// PermanentRedirect sends a redirection response HTTP 301.
func (t *Transaction) PermanentRedirect(url string) {
	t.Redirect(url, http.StatusMovedPermanently)
}

// PermanentRedirectSameMethod sends a redirection response HTTP 308.
// The new location will be requested using the same method as the current request.
func (t *Transaction) PermanentRedirectSameMethod(url string) {
	code := http.StatusMovedPermanently
	if t.Request.ProtoAtLeast(1, 1) {
		code = http.StatusPermanentRedirect
	}
	t.Redirect(url, code)
}

// ReferrerURL returns a URL of the "referer" request field if present.
// Returns fallback if the value of the "referer" header is not a valid URL.
func (t *Transaction) ReferrerURL(fallback *url.URL) *url.URL {
	referrer := t.Request.Header.Get("referer") // yup, it's misspelled
	if referrer != "" {
		refurl, err := url.Parse(referrer)
		if err == nil {
			return refurl
		}
	}
	return fallback
}

// DifferentReferrerURL returns a URL of the "referer" request field if present.
// If the referrer's path is the same as t.URL.Path then nil is returned.
func (t *Transaction) DifferentReferrerURL() *url.URL {
	referrer := t.ReferrerURL(nil)
	if referrer != nil && referrer.Path != t.URL.Path {
		return referrer
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
