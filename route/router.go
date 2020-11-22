package route

import (
	"fmt"
	"strings"
)

type Router struct {
	// BasePath is the URL path prefix where these routes begin.
	// All rules within are relative to this path.
	BasePath string
	Routes   []Route
}

func (r *Router) Add(pattern string, handler interface{}) (*Route, error) {
	// perform some generic checks on Router, since Add is called a lot less often than ServeHTTP.
	if len(r.BasePath) > 0 {
		if r.BasePath == "/" {
			r.BasePath = ""
		} else if r.BasePath[0] != '/' {
			// the Dispatch function expects BasePath to start with "/"; add it
			r.BasePath = "/" + r.BasePath
		} else {
			// the Dispatch function expects BasePath to not end in "/"; trim away
			for i := len(r.BasePath) - 1; i >= 0 && r.BasePath[i] == '/'; i-- {
				r.BasePath = r.BasePath[:i]
			}
		}
	}

	// new Route
	r.Routes = append(r.Routes, Route{})
	route := &(r.Routes[len(r.Routes)-1])

	// parse
	if err := route.Parse(pattern); err != nil {
		r.Routes = r.Routes[:len(r.Routes)-1]
		return nil, err
	}

	route.Handler = handler
	return route, nil
}

func (r *Router) Match(conditions CondFlags, path string) (*Match, error) {
	// trim BasePath off of URL path
	if len(r.BasePath) > 0 {
		// when BasePath is non-empty it...
		// - always begins with "/"
		// - never ends with "/"
		// - is never just "/"
		//
		if !strings.HasPrefix(path, r.BasePath) {
			return nil, fmt.Errorf("requested path %q outside of BasePath %q", path, r.BasePath)
		}
		path = path[len(r.BasePath):]
	}

	// This could be a lot more efficient with something fancy like a b-tree.
	// For now, keep it simple and just do a linear scan.
	for i := range r.Routes {
		route := &r.Routes[i]

		// check conditions
		if route.Conditions != 0 && (route.Conditions&conditions) == 0 {
			continue
		}

		// check constant prefix
		if len(route.EntryPrefix) > 0 && !strings.HasPrefix(path, route.EntryPrefix) {
			continue
		}

		if route.Pattern == nil {
			// no variables
			if route.IsPrefix || path == route.EntryPrefix {
				return &Match{Route: route, Path: path}, nil
			}
		} else {
			// check regexp
			values := route.Pattern.FindStringSubmatch(path)
			if len(values) == 1+len(route.Vars) {
				return &Match{Route: route, Path: path, values: values[1:]}, nil
			}
		}
	}

	// no route found
	return nil, nil
}
