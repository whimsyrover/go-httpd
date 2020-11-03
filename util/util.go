package util

import (
  "errors"
  "net/http"
  "net/textproto"
  "strings"
)

// HeaderSetCookie sets or adds a cookie in or to the provided HTTP header.
// This is different from header.Set("Set-Cookie") as it replaces cookies with the same
// cookie name but adds cookies with different names.
//
// In other words, if you call:
//   HeaderSetCookie("foo=123;...")
//   HeaderSetCookie("bar=456;...")
//   HeaderSetCookie("foo=789;...") // replaces foo cookie
// then the actual header will contain:
//   Set-Cookie: foo=789;...
//   Set-Cookie: bar=456;...
//
func HeaderSetCookie(header http.Header, cookie string) error {
  name := parseCookieName(cookie)
  if len(name) == 0 {
    return errors.New("invalid cookie")
  }
  // find existing cookie with same name
  existingCookies := header.Values("Set-Cookie")
  for i, line := range existingCookies {
    name2 := parseCookieName(line)
    if name == name2 {
      // replace cookie
      existingCookies[i] = cookie
      return nil
    }
  }
  // cookie not yet set in header; add it
  header.Add("Set-Cookie", cookie)
  return nil
}

// returns the name in a string like "name=value;..."
func parseCookieName(cookie string) string {
  i := strings.IndexByte(cookie, ';')
  if i < 0 {
    return ""
  }
  e := strings.IndexByte(cookie, '=')
  if e < 0 || e > i {
    return ""
  }
  return textproto.TrimString(cookie[:e])
}
