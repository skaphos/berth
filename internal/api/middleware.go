package api

import "net/http"

// ChainMiddleware wraps handler with the given middleware functions in
// left-to-right order. The first middleware in the list is the outermost
// wrapper.
func ChainMiddleware(handler http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	wrapped := handler
	for i := len(middleware) - 1; i >= 0; i-- {
		wrapped = middleware[i](wrapped)
	}
	return wrapped
}
