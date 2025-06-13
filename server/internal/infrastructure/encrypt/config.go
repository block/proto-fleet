package encrypt

type Config struct {
	ServiceMasterKey string `help:"Service Master key used for encryption." env:"SERVICE_MASTER_KEY" required:""`
}
