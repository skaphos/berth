package api

import "net/http"

func ChainMiddleware(handler http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	wrapped := handler
	for i := len(middleware) - 1; i >= 0; i-- {
		wrapped = middleware[i](wrapped)
	}
	return wrapped
}
