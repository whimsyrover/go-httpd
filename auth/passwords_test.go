package auth

import (
	"fmt"
	"testing"

	"github.com/rsms/go-testutil"
)

type TestAccounts struct {
	Passwords
	db map[int]*TestAccount
}

type TestAccount struct {
	id           int
	passwordData []byte
}

func TestPasswords(t *testing.T) {
	assert := testutil.NewAssert(t)
	assert.Ok("a", true)

	var accounts TestAccounts
	accounts.Passwords.Config = DefaultConfig
	accounts.db = make(map[int]*TestAccount)

	accounts.Passwords.SetAccountPasswordData = func(id interface{}, data []byte) error {
		a := accounts.db[id.(int)]
		if a == nil {
			return fmt.Errorf("account %v not found", a)
		}
		a.passwordData = data
		return nil
	}

	accounts.Passwords.GetAccountPasswordData = func(id interface{}) ([]byte, error) {
		a := accounts.db[id.(int)]
		if a == nil {
			return nil, fmt.Errorf("account %v not found", a)
		}
		return a.passwordData, nil
	}

	account1 := &TestAccount{id: 1}
	account2 := &TestAccount{id: 2}
	accounts.db[account1.id] = account1
	accounts.db[account2.id] = account2

	err := accounts.Passwords.Set(account1.id, "lolcat")
	assert.NoErr("Passwords.Set account1", err)

	err = accounts.Passwords.Verify(account1.id, "lolcat")
	assert.NoErr("Passwords.Verify account1", err)

	// account2 has no password data so the error message is "invalid account"
	err = accounts.Passwords.Verify(account2.id, "lolcat")
	assert.Err("Passwords.Verify account2 with wrong password", "invalid account", err)

	err = accounts.Passwords.Set(account2.id, "hotdog")
	assert.NoErr("Passwords.Set account2", err)

	err = accounts.Passwords.Verify(account2.id, "hotdog")
	assert.NoErr("Passwords.Verify account2", err)

	// account2 now has password data so the error message should be different
	err = accounts.Passwords.Verify(account2.id, "lolcat")
	assert.Err("Passwords.Verify account2 with wrong password", "invalid password", err)

	// change password
	err = accounts.Passwords.Change(account1.id, "lolcat", "monorail")
	assert.NoErr("Passwords.Change account1", err)

	// change password fails with the wrong "currentPassword" argument
	err = accounts.Passwords.Change(account1.id, "lolcat", "monorail")
	assert.Err("Passwords.Change account1", "invalid password", err)
}
