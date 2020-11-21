package session

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"net/http"
	"time"

	"github.com/rsms/go-httpd/util"
	"github.com/rsms/go-uuid"
)

// Session holds a set of keys & values associated with an ID
type Session struct {
	ID string // globally unique session identifier

	store  *Store                 // parent store
	values map[string]interface{} // cached values (including pending changes, if dirty=true)
	dirty  bool                   // true if values have been modified
}

func (s *Session) String() string {
	return s.ID
}

func (s *Session) Set(key string, value interface{}) {
	if s.values == nil {
		if value != nil {
			s.values = map[string]interface{}{key: value}
			s.dirty = true
		}
	} else {
		if value == nil {
			delete(s.values, key)
		} else {
			s.values[key] = value
		}
		s.dirty = true
	}
}

func (s *Session) Get(key string) interface{} {
	if s.values != nil {
		return s.values[key]
	}
	return nil
}

func (s *Session) Del(key string) {
	if s.values != nil {
		if _, ok := s.values[key]; ok {
			delete(s.values, key)
			s.dirty = true
		}
	}
}

// Clear removes all data for the session.
// A subsequent call to Save or SaveHTTP will remove the session data from the db
// (and the cookie from the HTTP client in case of calling SaveHTTP.)
func (s *Session) Clear() {
	if len(s.values) != 0 {
		s.values = nil
		s.dirty = true
	}
}

func (s *Session) LoadHTTP(r *http.Request) error {
	c, err := r.Cookie(s.store.CookieName)
	if err != nil {
		return err
	}
	id := c.Value
	if !isValidSessionID(id) {
		return fmt.Errorf("invalid session id in session cookie")
	}
	return s.Load(id)
}

// SaveHTTP persists the session's data if needed and refreshes its expiration by
// calling Save() and then sets a corresponding cookie in the response header.
//
// SaveHTTP should be called after a session's Set or Del methods have been called.
//
// Note that a session in a Transaction is saved automatically.
//
// If the session does not have an ID (i.e. the session is new), then s.ID
// is assigned a new identifier in the case the session has any data.
//
func (s *Session) SaveHTTP(w http.ResponseWriter) error {
	// Save() might clear s.dirty and/or s.ID so check if s will modify db storage
	// before we call Save()
	shouldSetCookie := s.dirty || len(s.ID) > 0

	// Set if dirty and refresh TTL
	if err := s.Save(); err != nil || !shouldSetCookie {
		// either save failed or we only needed to refresh TTL
		return err
	}

	// Set cookie
	cookie := s.bakeSessionIDCookie()
	return util.HeaderSetCookie(w.Header(), cookie)
}

// bakeSessionIDCookie creates a cookie named s.store.CookieName
// with max-age s.store.TTL and value s.ID
func (s *Session) bakeSessionIDCookie() string {
	// See https://tools.ietf.org/html/rfc6265

	// MaxAge=0 means no Max-Age attribute specified and the cookie will be
	// deleted after the browser session ends.
	// MaxAge<0 means delete cookie immediately.
	// MaxAge>0 means Max-Age attribute present and given in seconds.
	maxAgeSec := -1
	if len(s.values) > 0 {
		maxAgeSec = int(s.store.TTL / time.Second)
	}

	cookie := fmt.Sprintf("%s=%s;Path=/;Max-Age=%d;HttpOnly;SameSite=Strict",
		s.store.CookieName,
		s.ID,
		maxAgeSec,
	)
	// Note: "HttpOnly" = don't expose to javascript

	// "Secure" instructs the requestor to only store this cookie if the connection over which
	// it's transmitted is secure (i.e. only over HTTPS.)
	if !s.store.AllowInsecureCookies {
		cookie += ";Secure"
	}

	return cookie
}

// Load restores session data for id. s.ID is assigned id on success and "" on error.
//
func (s *Session) Load(id string) error {
	data, err := s.store.storage.GetSessionData(id)
	s.ID = ""
	if err == nil && len(data) > 0 {
		var values map[string]interface{}
		values, err = decodeSessionValues(data)
		if err == nil {
			s.values = values
			s.ID = id
		}
	}
	return err
}

// Save persists the session's data if needed and refreshes its expiration.
// If the session does not have an ID (i.e. the session is new), then s.ID
// is assigned a new identifier in the case the session has any data.
//
func (s *Session) Save() (err error) {
	if s.dirty {
		var data []byte
		if len(s.values) == 0 {
			err = s.store.storage.DelSessionData(s.ID)
			if err == nil {
				s.ID = ""
			}
		} else if data, err = encodeSessionValues(s.values); err == nil {
			if len(s.ID) == 0 {
				id, err1 := uuid.Gen()
				if err1 != nil {
					err = err1
				} else {
					s.ID = id.String()
				}
			}
			err = s.store.storage.SetSessionData(s.ID, data, s.store.TTL)
		}
		if err == nil {
			s.dirty = false
		}
	} else if len(s.ID) > 0 {
		// refresh timestamp in db
		err = s.store.storage.RefreshSessionData(s.ID, s.store.TTL)
	}
	return
}

func decodeSessionValues(data []byte) (values map[string]interface{}, err error) {
	buf := bytes.NewBuffer(data)
	err = gob.NewDecoder(buf).Decode(&values)
	return
}

func encodeSessionValues(values map[string]interface{}) ([]byte, error) {
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(values)
	return buf.Bytes(), err
}

func isValidSessionID(id string) bool {
	if len(id) < 4 || len(id) > uuid.StringMaxLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		b := id[i]
		if !((b >= '0' && b <= '9') || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') ||
			b == '-' || b == '_') {
			// invalid byte
			return false
		}
	}
	return true
}
