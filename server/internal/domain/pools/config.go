package pools

import "time"

type Config struct {
	timeout time.Duration `help:"Timeout for pool operations" default:"10s" env:"TIMEOUT"`
}
