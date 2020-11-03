package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/crypto/scrypt"
)

// scrypt constants
type Config struct {
	N int // scrypt CPU/memory cost parameter, which must be a power of two greater than 1.
	R int // scrypt block size parameter (must satisfy R * P < 2^30)
	P int // scrypt parallelisation parameter (must satisfy R * P < 2^30)

	SaltLen int // length of generated salt, in bytes
	HashLen int // length of generated hash, in bytes
}

// DefaultConfig holds the default configuration parameters.
// The recommended parameters for interactive logins as of 2017 are N=32768, r=8 and p=1.
var DefaultConfig = Config{
	N:       32768, // CPU/memory cost parameter
	R:       8,     // block size parameter
	P:       1,     // parallelisation parameter
	SaltLen: 32,
	HashLen: 32,
}

// ErrInvalidPassword is returned by CheckPassword is the input password is not a match
var ErrInvalidPassword = errors.New("invalid password")

// HashPassword takes an input password and a salt, returning a hash (or "derived key").
// The hash returned can together with the input salt be used to verify a password using
// CheckPassword.
//
// In case you need different groups of passwords, you could hash the password with a
// private key before passing it to HashPassword:
//
//   import (
//     "crypto/hmac"
//     "crypto/sha256"
//   )
//   hm := hmac.New(sha256.New, privateKey)
//   _, err := hm.Write(password)
//   if err != nil {
//     return nil, err
//   }
//   return HashPassword(hm.Sum(nil), salt)
//
func (c Config) HashPassword(password, salt []byte) ([]byte, error) {
	return scrypt.Key(password, salt, c.N, c.R, c.P, c.HashLen)
}

// CheckPassword verifies a password; returns nil if password is correct
func (c Config) CheckPassword(password, salt, hash []byte) error {
	hash2, err := HashPassword(password, salt)
	if err == nil {
		if subtle.ConstantTimeCompare(hash2, hash) != 1 {
			err = ErrInvalidPassword
		}
	}
	return err
}

// GenSalt generates a new cryptographically-strong salt to be used with HashPassword
func (c Config) GenSalt() ([]byte, error) {
	salt := make([]byte, c.SaltLen)
	_, err := rand.Read(salt)
	return salt, err
}

// Encode config along with salt and hash, returning base-64 data
func (c Config) Encode(salt, hash []byte) []byte {
	b := c.EncodeRaw(salt, hash)
	out := make([]byte, base64.RawStdEncoding.EncodedLen(len(b)))
	base64.RawStdEncoding.Encode(out, b)
	return out
}

// Decode decodes a base-64 encoded config, salt and hash
// previously encoded with c.Encode
func Decode(data []byte) (c Config, salt, hash []byte, err error) {
	b := make([]byte, base64.RawStdEncoding.DecodedLen(len(data)))
	_, err = base64.RawStdEncoding.Decode(b, data)
	if err != nil {
		return
	}
	return DecodeRaw(b)
}

// EncodeRaw encodes the config along with salt and hash
func (c Config) EncodeRaw(salt, hash []byte) []byte {
	z := binary.MaxVarintLen32*5 + len(salt) + len(hash)
	b := make([]byte, z)

	i := binary.PutUvarint(b, uint64(c.N))
	i += binary.PutUvarint(b[i:], uint64(c.R))
	i += binary.PutUvarint(b[i:], uint64(c.P))
	i += binary.PutUvarint(b[i:], uint64(len(salt)))
	i += binary.PutUvarint(b[i:], uint64(len(hash)))
	copy(b[i:], salt)
	i += len(salt)
	copy(b[i:], hash)
	i += len(hash)

	return b[:i]
}

// DecodeRaw decodes a base-64 encoded config, salt and hash previously encoded with c.EncodeRaw
func DecodeRaw(data []byte) (c Config, salt, hash []byte, err error) {
	b := data

	N, n := binary.Uvarint(b)
	i := n
	R, n := binary.Uvarint(b[i:])
	i += n
	P, n := binary.Uvarint(b[i:])
	i += n
	SaltLen, n := binary.Uvarint(b[i:])
	i += n
	HashLen, n := binary.Uvarint(b[i:])
	if n <= 0 {
		err = fmt.Errorf("invalid data (header)")
	}
	i += n
	c.N = int(N)
	c.R = int(R)
	c.P = int(P)
	c.SaltLen = int(SaltLen)
	c.HashLen = int(HashLen)
	salt = b[i : i+c.SaltLen]
	i += c.SaltLen
	hash = b[i : i+c.HashLen]
	return
}

// Package-level functions on DefaultConfig:

// HashPassword takes an input password and a salt, returning a hash
func HashPassword(password, salt []byte) ([]byte, error) {
	return DefaultConfig.HashPassword(password, salt)
}

// CheckPassword verifies a password; returns nil if password is correct
func CheckPassword(password, salt, hash []byte) error {
	return DefaultConfig.CheckPassword(password, salt, hash)
}

// GenSalt generates a new cryptographically-strong salt to be used with HashPassword
func GenSalt() ([]byte, error) {
	return DefaultConfig.GenSalt()
}

// Encode config along with salt and hash, returning base-64 data
func Encode(salt, hash []byte) []byte {
	return DefaultConfig.Encode(salt, hash)
}
