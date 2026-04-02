package web

import "github.com/block/proto-fleet/server/sdk/v1"

type DigestAuth struct {
	creds     sdk.UsernamePassword
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
