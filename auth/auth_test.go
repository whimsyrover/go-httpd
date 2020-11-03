package auth

import (
	"testing"
	"time"

	"github.com/rsms/go-testutil"
)

func Test_init(t *testing.T) {
	// These tests uses intentionally-slow computations which means timeout can't be too short
	deadline, usesTimeout := t.Deadline()
	if usesTimeout && time.Until(deadline) < 10*time.Second {
		t.Errorf(
			"This test suite needs to be run with a longer timeout since password verification"+
				" makes use of timing. Tests usually need 2-5s to complete. (Current timeout in %s)",
			time.Until(deadline),
		)
	}
}

func TestFundamentals(t *testing.T) {
	assert := testutil.NewAssert(t)

	password1 := []byte("lolcat")
	password2 := []byte("hotdog")

	salt, err := GenSalt()
	assert.NoErr("GenSalt", err)
	assert.Eq("GenSalt generated SaltLen long salt", DefaultConfig.SaltLen, len(salt))

	hash, err := HashPassword(password1, salt)
	assert.NoErr("HashPassword", err)
	assert.Eq("HashPassword generates HashLen long hash", DefaultConfig.HashLen, len(hash))

	err = CheckPassword(password1, salt, hash)
	assert.NoErr("CheckPassword succeeded", err)

	err = CheckPassword(password2, salt, hash)
	assert.Err("CheckPassword failed with wrong password", "invalid password", err)

	err = CheckPassword(password1, append(salt, 'x'), hash)
	assert.Err("CheckPassword failed with different salt", "invalid password", err)

	err = CheckPassword(password1, salt, append(hash, 'x'))
	assert.Err("CheckPassword failed with different hash", "invalid password", err)
}

func TestConfigEncode(t *testing.T) {
	assert := testutil.NewAssert(t)
	password1 := []byte("lolcat")

	salt, err := GenSalt()
	assert.NoErr("GenSalt", err)
	hash, err := HashPassword(password1, salt)
	assert.NoErr("HashPassword", err)

	config1 := DefaultConfig

	data := config1.Encode(salt, hash)
	t.Logf("config1.Encode => %q", data)

	config2, salt2, hash2, err := Decode(data)
	assert.NoErr("Decode", err)
	assert.Eq("config N", config2.N, config1.N)
	assert.Eq("config R", config2.R, config1.R)
	assert.Eq("config P", config2.P, config1.P)
	assert.Eq("config SaltLen", config2.SaltLen, config1.SaltLen)
	assert.Eq("config HashLen", config2.HashLen, config1.HashLen)
	assert.Eq("salt2", salt, salt2)
	assert.Eq("hash2", hash, hash2)
}
