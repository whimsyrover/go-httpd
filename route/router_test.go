package route

import (
	"testing"

	"github.com/rsms/go-testutil"
)

func TestRouter(t *testing.T) {
	assert := testutil.NewAssert(t)

	var r Router

	r.BasePath = "/foo"

	r.Add(`/us.er/{id:[0-9a-zA-Z]+}/{action:\w+}/thing`, 1)
	r.Add("/us.er/{q}", 2)
	r.Add("GET|POST /", 99)

	m, err := r.Match(CondMethodPUT, r.BasePath+"/")
	assert.NoErr("no input error", err)
	assert.Ok("PUT method not in condition for '/'", m == nil)

	m, err = r.Match(CondMethodGET, r.BasePath+"/")
	assert.NoErr("no input error", err)
	assert.Eq("route 99", m.Handler.(int), 99)

	m, err = r.Match(CondMethodGET, r.BasePath+"/us.er/bob")
	assert.NoErr("no input error", err)
	assert.Eq("route 2", m.Handler.(int), 2)

	m, err = r.Match(CondMethodGET, r.BasePath+"/us.er/bob/lol/thing")
	assert.NoErr("no input error", err)
	assert.Eq("route 1", m.Handler.(int), 1)

	assert.Eq("Var", m.Var("id"), "bob")
	assert.Eq("Var", m.Var("action"), "lol")
	assert.Eq("Var", m.Var("unknown"), "")

	assert.Eq("Values", len(m.Values()), 2)
	assert.Eq("Values", m.Values()[0], "bob")
	assert.Eq("Values", m.Values()[1], "lol")

	assert.Eq("Vars", len(m.Vars()), 2)
	assert.Eq("Vars", m.Vars()["id"], "bob")
	assert.Eq("Vars", m.Vars()["action"], "lol")
}
