package session

import (
	"errors"
	"sync"
	"time"
)

// Storage provides persistance for session data.
//
// The information stored in a session is usually sensitive, i.e. it may contain user
// authentication data or server-private details like internal IDs. For this reason a
// Storage implementation should do its best to store data in a secure manner.
//
type Storage interface {
	// GetSessionData retrieves data for a session.
	// It returns nil if not found along with a decriptive error.
	GetSessionData(sessionId string) ([]byte, error)

	// SetSessionData adds or replaces data for a session.
	//
	// The TTL value is relative to "now" and dictates the maximum age of session data.
	// The implementation may choose how to enforce this requirement. Storage backed by for
	// example memcached or redis could use the built-in TTL functionality of those storage
	// mechanisms while a simpler implementation could store the time + ttl along with the
	// data and check if data is expired or not in its GetSessionData method.
	//
	SetSessionData(sessionId string, data []byte, ttl time.Duration) error

	// RefreshSessionData is similar to SetSessionData and is used to extend the expiration
	// time of a session without changing its data.
	//
	// This method is usually called whenever a session has been used (accessed) and so
	// implementations should make sure this is efficient. A trivial implementation, or an
	// implementation not concerned with performance, may implement this method as a call to
	// GetSessionData followed by a call to SetSessionData.
	//
	RefreshSessionData(sessionId string, ttl time.Duration) error

	// DelSessionData removes any data associated with sessionId.
	// If there is no data for sessionId then nil is returned rather than a "not found" error.
	// An error is returned when there was data for sessionId but the delete operation failed.
	DelSessionData(sessionId string) error
}

// MemoryStorage is an implementation of Storage that keeps session data in memory.
// Useful for testing and also demonstrates a concrete implementation.
type MemoryStorage struct {
	sync.Map
}

type memStorageEntry struct {
	data    []byte
	expires time.Time
}

var (
	ErrStorageNotFound = errors.New("not found")
	ErrStorageExpired  = errors.New("expired")
)

func (s *MemoryStorage) GetSessionData(sessionId string) ([]byte, error) {
	if v, ok := s.Load(sessionId); ok {
		entry := v.(memStorageEntry)
		if time.Now().After(entry.expires) {
			s.Delete(sessionId)
			return nil, ErrStorageExpired
		}
		return entry.data, nil
	}
	return nil, ErrStorageNotFound
}

func (s *MemoryStorage) SetSessionData(sessionId string, data []byte, ttl time.Duration) error {
	s.Store(sessionId, memStorageEntry{data, time.Now().Add(ttl)})
	return nil
}

func (s *MemoryStorage) RefreshSessionData(sessionId string, ttl time.Duration) error {
	v, ok := s.Load(sessionId)
	if !ok {
		return ErrStorageNotFound
	}
	entry := v.(memStorageEntry)
	entry.expires = time.Now().Add(ttl)
	s.Store(sessionId, entry)
	return nil
}

func (s *MemoryStorage) DelSessionData(sessionId string) error {
	s.Delete(sessionId)
	return nil
}
