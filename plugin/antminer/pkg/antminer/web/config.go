package web

import (
	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/networking"
	"github.com/btc-mining/proto-fleet/server/sdk/v1"
)

type AntminerConnectionInfo struct {
	networking.ConnectionInfo
	Creds sdk.UsernamePassword
}

func NewAntminerConnectionInfo(connectionInfo networking.ConnectionInfo, credential sdk.UsernamePassword) *AntminerConnectionInfo {
	return &AntminerConnectionInfo{
		ConnectionInfo: connectionInfo,
		Creds:          credential,
	}
}
