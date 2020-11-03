package auth

import (
	"errors"
)

// ErrInvalidAccount is returned by Change and Verify if there is no password data for the
// requested account. Usually this means that the account does not exist.
var ErrInvalidAccount = errors.New("invalid account")

// Passwords represents a group of passwords
type Passwords struct {
	// Config is the crypto configuration used when setting or changing password.
	// This must be initialized before using any of the methods of this type.
	Config

	// SetAccountPasswordData is called to store password data for an account identified by a.
	// It's called as a result from calling Passwords.Set or Passwords.Change
	// The data is base-64 encoded and can thus safely be printed or transmitted via e.g. JSON.
	SetAccountPasswordData func(a interface{}, data []byte) error

	// GetAccountPasswordData is called to load password data for an account identified by a.
	// It must return data previously stored via SetAccountPasswordData.
	// It should return an error if no data exists for the account, however an implementation
	// can choose to return nil or an empty byte slice in this case instead, which leads to
	// ErrInvalidAccount being returned from the calling function.
	GetAccountPasswordData func(a interface{}) ([]byte, error)
}

// Set computes a hash from salt + password and assigns the result to the account identified by a.
// This is usually used when creating new accounts or during password recovery.
func (s *Passwords) Set(a interface{}, password string) error {
	// generate a salt
	salt, err := s.Config.GenSalt()
	if err != nil {
		return err
	}
	// compute derived key which we call "hash"
	hash, err := s.Config.HashPassword([]byte(password), salt)
	if err != nil {
		return err
	}
	// encode config, salt and hash
	data := s.Config.Encode(salt, hash)
	return s.SetAccountPasswordData(a, data)
}

// Verify checks if the provided password is correct for the account identified by a.
// This is usually used during sign in.
func (s *Passwords) Verify(a interface{}, password string) error {
	data, err := s.GetAccountPasswordData(a)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return ErrInvalidAccount
	}
	c, salt, hash, err := Decode(data)
	if err != nil {
		return err
	}
	return c.CheckPassword([]byte(password), salt, hash)
}

// Change is like a conditional Set: sets the password of account identified by a to newPassword
// only if the currentPassword passes Verify.
// It's essentially a wrapper around Verify() & Set().
// This is usually used when the user changes their password.
func (s *Passwords) Change(a interface{}, currentPassword, newPassword string) error {
	err := s.Verify(a, currentPassword)
	if err != nil {
		return err
	}
	return s.Set(a, newPassword)
}
