package session

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Store represents a domain of sessions. Usually an app has just one of these.
// A Store manages Sessions and persists their data using Storage.
type Store struct {
	// TTL defines the time-to-live for sessions; how old a session can be before it
	// expires & is considered dead. This is relative to the last Save() call.
	TTL time.Duration // defaults to 30 days

	// CookieName is the name of the HTTP cookie to use for session ID transmission
	CookieName string // defaults to "session"

	// AllowInsecureCookies can be set to true to omit the "Secure" directive in cookies.
	// This is needed for cookies to "stick" when serving over unencrypted http (i.e. no TLS.)
	AllowInsecureCookies bool

	storage Storage
}

func NewStore(storage Storage) *Store {
	ss := &Store{}
	ss.SetStorage(storage)
	return ss
}

// Storage returns the storage used by this store
func (ss *Store) Storage() Storage { return ss.storage }

// SetStorage sets the storage used for persistance.
// This method also initializes TTL and CookieName to default values, if they are empty.
func (ss *Store) SetStorage(storage Storage) {
	ss.storage = storage
	if ss.TTL == 0 {
		ss.TTL = 30 * 24 * time.Hour // 30 days
	}
	if ss.CookieName == "" {
		ss.CookieName = "session"
	}
}

// GetHTTP retrieves a session from a http request.
// The results are cached for the same request making this function efficient to call frequently.
func (ss *Store) GetHTTP(r *http.Request) *Session {
	var ctx = r.Context()
	v := ctx.Value(ss)
	if v != nil {
		return v.(*Session)
	}
	s, _ := ss.LoadHTTP(r)                             // ignore error
	*r = *r.WithContext(context.WithValue(ctx, ss, s)) // cache
	return s
}

// LoadHTTP attempts to load a session from a http request by reading session id from cookie and
// loading the session data from storage.
//
// A valid Session object is always returned. The returned error value indicates if loading of a
// session succeeded (err=nil) and should be considered informative rather than a hard error.
//
func (ss *Store) LoadHTTP(r *http.Request) (*Session, error) {
	s := &Session{store: ss}
	if ss.storage == nil {
		return s, ErrNoStorage
	}
	return s, s.LoadHTTP(r)
}

// ErrNoStorage is returned when loading a session with a Store that has no backing storage
var ErrNoStorage = errors.New("no session storage configured")
