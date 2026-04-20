package server

import "net/http"

type Middleware interface {
	Wrap(handler http.Handler) http.Handler
}
