package minerclient

import "net/url"

type MinerConnectionInfo struct {
	URL       *url.URL
	AuthToken string
}
