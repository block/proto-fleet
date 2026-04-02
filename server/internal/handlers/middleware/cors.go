package middleware

import (
	"net/http"

	"github.com/block/proto-fleet/server/internal/infrastructure/server"
	"github.com/rs/cors"
)

type CORSMiddleware struct {
	cors *cors.Cors
}

func (c CORSMiddleware) Wrap(handler http.Handler) http.Handler {
	return c.cors.Handler(handler)
}

var _ server.Middleware = CORSMiddleware{}

func NewCORSMiddleware(suppressCors bool) *CORSMiddleware {
	var middleware *cors.Cors

	if suppressCors {
		middleware = cors.New(cors.Options{
			AllowedOrigins:   []string{"*"},
			AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Authorization", "Content-Type", "Content-Range", "Accept", "Connect-Protocol-Version"},
			AllowCredentials: true,
		})
	} else {
		middleware = cors.New(cors.Options{})
	}

	return &CORSMiddleware{cors: middleware}
}
