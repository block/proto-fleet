package middleware

import (
	connectcors "connectrpc.com/cors"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/server"
	"github.com/rs/cors"
	"net/http"
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
			AllowedOrigins: []string{"*"},
			AllowedMethods: connectcors.AllowedMethods(),
			AllowedHeaders: connectcors.AllowedHeaders(),
			ExposedHeaders: connectcors.ExposedHeaders(),
		})
	} else {
		middleware = cors.New(cors.Options{})
	}

	return &CORSMiddleware{cors: middleware}
}
