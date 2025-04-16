package token

import "time"

type Config struct {
	SecretKey        string        `help:"Secret key for signing the JWT" env:"SECRET_KEY"`
	ExpirationPeriod time.Duration `help:"Expiration period duration for the JWT" env:"EXPIRATION_PERIOD"`
}
