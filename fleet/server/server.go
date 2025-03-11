package server

import (
	"database/sql"
	"github.com/btc-mining/miner-firmware/fleet/api"
	"github.com/btc-mining/miner-firmware/fleet/api/gen/authors/v1/authorsv1connect"
	"github.com/btc-mining/miner-firmware/fleet/api/gen/greet/v1/greetv1connect"
	"github.com/btc-mining/miner-firmware/fleet/db/sqlc"
	"net/http"
)

// NewMux allocates and instantiates a new http.ServeMux that serves protofleet HTTP routes
func NewMux(staticAssetPath string, conn *sql.DB, q *sqlc.Queries) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(staticAssetPath)))

	mux.Handle(greetv1connect.NewGreetServiceHandler(&api.GreetServer{}))
	mux.Handle(authorsv1connect.NewAuthorsServiceHandler(api.NewAuthorsServer(conn, q)))
	return mux
}
