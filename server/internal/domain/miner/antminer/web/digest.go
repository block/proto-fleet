package web

import "github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"

type DigestAuth struct {
	Username  string
	Password  secrets.Text
	Realm     string
	Nonce     string
	URI       string
	Algorithm string
	Response  string
	Opaque    string
	QOP       string
	NC        string
	CNonce    string
}

type DigestChallenge struct {
	Realm     string
	Nonce     string
	Opaque    string
	Algorithm string
	QOP       string
}
