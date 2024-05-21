package httpd

import (
	"github.com/rsms/go-httpd/route"
)

type handlerFunc func(*Transaction)

func (f handlerFunc) ServeHTTP(t *Transaction) { f(t) }

// Router is a HTTP-specific kind of route.Router
type Router struct {
	route.Router
}

func (r *Router) HandleFunc(pattern string, f func(*Transaction)) (*route.Route, error) {
	return r.Handle(pattern, handlerFunc(f))
}

func (r *Router) Handle(pattern string, handler Handler) (*route.Route, error) {
	return r.Add(pattern, handler)
}

func (r *Router) Match(t *Transaction) (Handler, error) {
	// effective conditions of the transaction
	conditions, _ := route.ParseCondFlags([]string{t.Method()})

	// find a matching route
	m, err := r.Router.Match(conditions, t.URL.Path)
	if err != nil || m == nil {
		return nil, err
	}
	t.routeMatch = m
	return m.Route.Handler.(Handler), nil
}

func (r *Router) ServeHTTP(t *Transaction) {
	if !r.MaybeServeHTTP(t) {
		t.RespondWithStatusNotFound()
	}
}


