package main

import (
	"time"

	"github.com/proto-at-block/proto-fleet/server/internal/domain/command"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/diagnostics"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/ipscanner"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/plugins"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/pools"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/session"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/telemetry"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/telemetry/scheduler"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/token"
	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/db"
	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/files"
	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/logging"
	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/timescaledb"
)

type HTTPConfig struct {
	Address           string        `help:"Address to listen on" default:"127.0.0.1:8080" env:"LISTEN_ADDRESS"`
	ReadHeaderTimeout time.Duration `help:"Read header timeout" default:"3s" env:"READ_HEADER_TIMEOUT"`
	SuppressCors      bool          `help:"Suppress CORS" default:"false" env:"SUPPRESS_CORS"`
}
type Config struct {
	DB          db.Config          `embed:"" prefix:"db" envprefix:"DB_"`
	Log         logging.Config     `embed:"" prefix:"logging" envprefix:"LOG_"`
	HTTP        HTTPConfig         `embed:"" prefix:"http" envprefix:"HTTP_"`
	Auth        token.Config       `embed:"" prefix:"auth" envprefix:"AUTH_"`
	Session     session.Config     `embed:"" prefix:"session" envprefix:"SESSION_"`
	Pools       pools.Config       `embed:"" prefix:"pools" envprefix:"POOLS_"`
	Encrypt     encrypt.Config     `embed:"" prefix:"encrypt" envprefix:"ENCRYPT_"`
	Command     command.Config     `embed:"" prefix:"fleet_command" envprefix:"FLEET_COMMAND_"`
	Queue       queue.Config       `embed:"" prefix:"fleet_queue" envprefix:"FLEET_QUEUE_"`
	TimescaleDB timescaledb.Config `embed:"" prefix:"timescaledb" envprefix:"TIMESCALEDB_"`
	Telemetry   telemetry.Config   `embed:"" prefix:"telemetry" envprefix:"TELEMETRY_"`
	Scheduler   scheduler.Config   `embed:"" prefix:"scheduler" envprefix:"SCHEDULER_"`
	Plugins     plugins.Config     `embed:"" prefix:"plugins" envprefix:"PLUGINS_"`
	IPScanner   ipscanner.Config   `embed:"" prefix:"ipscanner" envprefix:"IPSCANNER_"`
	Diagnostics diagnostics.Config `embed:"" prefix:"diagnostics" envprefix:"DIAGNOSTICS_"`
	Files       files.Config       `embed:"" prefix:"files" envprefix:"FILES_"`
}
