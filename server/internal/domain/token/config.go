package token

import (
	"time"
)

type AuthTokenConfig struct {
	SecretKey        string        `help:"Secret key for signing the JWT" env:"SECRET_KEY" required:""`
	ExpirationPeriod time.Duration `help:"Expiration period duration for the JWT" env:"EXPIRATION_PERIOD" required:""`
}

type Config struct {
	ClientToken AuthTokenConfig `embed:"" prefix:"client-" envprefix:"CLIENT_"`
}
