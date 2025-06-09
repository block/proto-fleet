package main

import (
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/pairing"
	"github.com/btc-mining/proto-fleet/server/internal/domain/pools"
	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/logging"
)

type HTTPConfig struct {
	Address           string        `help:"Address to listen on" default:"127.0.0.1:8080" env:"LISTEN_ADDRESS"`
	ReadHeaderTimeout time.Duration `help:"Read header timeout" default:"3s" env:"READ_HEADER_TIMEOUT"`
	StaticAssetPath   string        `help:"Static asset path" env:"STATIC_ASSET_PATH"`
	SuppressCors      bool          `help:"Suppress CORS" default:"false" env:"SUPPRESS_CORS"`
}
type Config struct {
	DB      db.Config      `embed:"" prefix:"db" envprefix:"DB_"`
	Log     logging.Config `embed:"" prefix:"logging" envprefix:"LOG_"`
	HTTP    HTTPConfig     `embed:"" prefix:"http" envprefix:"HTTP_"`
	Auth    token.Config   `embed:"" prefix:"auth" envprefix:"AUTH_"`
	Pairing pairing.Config `embed:"" prefix:"pairing" envprefix:"PAIRING_"`
	Pools   pools.Config   `embed:"" prefix:"pools" envprefix:"POOLS_"`
	Encrypt encrypt.Config `embed:"" prefix:"encrypt" envprefix:"ENCRYPT_"`
}
