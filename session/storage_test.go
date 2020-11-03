package session

import (
	"testing"
	"time"

	"github.com/rsms/go-testutil"
)

func TestMemoryStorage(t *testing.T) {
	assert := testutil.NewAssert(t)

	var s MemoryStorage
	sessionId := "abc123"

	data, err := s.GetSessionData(sessionId)
	assert.Err("get non-existing should fail", "not found", err)
	assert.Eq("get non-existing should yield nil", data, nil)

	indata := []byte("hello")
	err = s.SetSessionData(sessionId, indata, time.Second)
	assert.NoErr("SetSessionData should succeed", err)

	data, err = s.GetSessionData(sessionId)
	assert.NoErr("GetSessionData should succeed after SetSessionData", err)
	assert.Eq("data", data, indata)

	err = s.DelSessionData(sessionId)
	assert.NoErr("DelSessionData should succeed", err)

	data, err = s.GetSessionData(sessionId)
	assert.Err("get non-existing should fail after DelSessionData", "not found", err)
	assert.Eq("get non-existing should yield nil after DelSessionData", data, nil)

	// expiry
	s.SetSessionData(sessionId, indata, time.Nanosecond /* expire immediately */)
	time.Sleep(time.Millisecond)
	data, err = s.GetSessionData(sessionId)
	assert.Err("get expired should fail", "expired", err)
	assert.Eq("get expired should yield nil", data, nil)

	// subsequent attempts to load should error with "not found" rather than "expired"
	_, err = s.GetSessionData(sessionId)
	assert.Err("get expired should fail", "not found", err)

	// expiration refresh
	// Note: The following two lines should be identical to the lines above under the "expiry"
	// comment.
	s.SetSessionData(sessionId, indata, time.Nanosecond /* expire immediately */)
	time.Sleep(time.Millisecond)
	// extend expiration time & fetch to verify
	err = s.RefreshSessionData(sessionId, time.Second)
	assert.NoErr("RefreshSessionData should succeed", err)
	// we should now be able to fetch the data within the next second
	data, err = s.GetSessionData(sessionId)
	assert.NoErr("GetSessionData should succeed after RefreshSessionData", err)
	assert.Eq("data", data, indata)
}
