package files

import "embed"

//go:embed migrations/*.sql
var Migrations embed.FS

//go:embed miner-configs/capabilities.yaml
var MinerConfigs embed.FS
