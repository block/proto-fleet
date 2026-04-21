package pools

import "time"

type Config struct {
	Timeout time.Duration `help:"Timeout for pool operations" default:"10s" env:"TIMEOUT"`
}
