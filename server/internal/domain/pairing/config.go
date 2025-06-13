package pairing

type Config struct {
	SecretKey string `help:"Secret key for signing the pairing tokens" env:"SECRET_KEY" required:""`
}
