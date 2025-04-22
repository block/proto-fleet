package main

import (
	auth2 "github.com/btc-mining/miner-firmware/fleet/internal/domain/token"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/db"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/logging"
	"time"
)

type HTTPConfig struct {
	Address           string        `help:"Address to listen on" default:"127.0.0.1:8080" env:"LISTEN_ADDRESS"`
	ReadHeaderTimeout time.Duration `help:"Read header timeout" default:"3s" env:"READ_HEADER_TIMEOUT"`
	StaticAssetPath   string        `help:"Static asset path" env:"STATIC_ASSET_PATH"`
}
type Config struct {
	DB   db.Config      `embed:"" prefix:"db" envprefix:"DB_"`
	Log  logging.Config `embed:"" prefix:"logging" envprefix:"LOG_"`
	HTTP HTTPConfig     `embed:"" prefix:"http" envprefix:"HTTP_"`
	Auth auth2.Config   `embed:"" prefix:"auth" envprefix:"AUTH_"`
}
