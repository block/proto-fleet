package pairing

type Config struct {
	NetworkSubnet string `help:"IPv4 subnet (CIDR) to report as the local network, overriding interface detection. Set it when fleetd runs in a container or VM whose interfaces are not on the miners' network." env:"NETWORK_SUBNET"`
}
