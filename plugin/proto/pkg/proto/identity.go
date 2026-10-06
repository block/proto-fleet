package proto

import (
	"context"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const pairingInfoPath = "/api/v1/pairing/info"

// ErrDeviceIdentity identifies a failure to verify the miner at the saved
// endpoint. It is an availability failure, not evidence that its password changed.
var ErrDeviceIdentity = errors.New("miner identity verification failed")

type identityError struct {
	reason string
}

func (e *identityError) Error() string { return ErrDeviceIdentity.Error() + ": " + e.reason }
func (e *identityError) Unwrap() error { return ErrDeviceIdentity }
func (e *identityError) GRPCStatus() *grpcstatus.Status {
	return grpcstatus.New(codes.Unavailable, e.Error())
}

type deviceIdentity struct {
	serial string
	mac    string
}

func newDeviceIdentity(serial, mac string) deviceIdentity {
	mac = strings.TrimSpace(mac)
	hw, err := net.ParseMAC(mac)
	if err != nil && len(mac) == 12 {
		var decoded []byte
		decoded, err = hex.DecodeString(mac)
		hw = net.HardwareAddr(decoded)
	}
	if err != nil {
		mac = ""
	} else {
		mac = strings.ToUpper(hw.String())
	}
	return deviceIdentity{serial: strings.TrimSpace(serial), mac: mac}
}

func (i deviceIdentity) usable() bool { return i.serial != "" || i.mac != "" }

// matches follows Fleet recovery's stable-identity rule: at least one shared
// identifier must match, and every identifier present on both sides must agree.
func (i deviceIdentity) matches(other deviceIdentity) bool {
	if i.serial != "" && other.serial != "" && i.serial != other.serial {
		return false
	}
	if i.mac != "" && other.mac != "" && i.mac != other.mac {
		return false
	}
	return (i.serial != "" && other.serial != "") || (i.mac != "" && other.mac != "")
}

// BindDeviceIdentity enables endpoint identity checks before credentials or API
// requests are sent. Call once before using the client. Unknown identity is
// learned on the first successful check; credentials changes never reset it.
// Discovery clients remain unbound so they can inspect unknown endpoints.
func (c *Client) BindDeviceIdentity(serial, mac string) {
	c.identityMu.Lock()
	defer c.identityMu.Unlock()
	identity := newDeviceIdentity(serial, mac)
	c.expectedIdentity = &identity
	// Identity and operations must address the same endpoint. In particular,
	// never follow a redirect carrying a password, bearer token, or mutation.
	c.httpClient = &http.Client{
		Transport:     c.httpClient.Transport,
		Timeout:       c.httpClient.Timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (c *Client) acceptIdentityLocked(observed deviceIdentity) error {
	if !observed.usable() {
		return &identityError{reason: "endpoint reported no stable identity"}
	}
	if c.expectedIdentity.usable() && !c.expectedIdentity.matches(observed) {
		return &identityError{reason: "endpoint does not match the expected miner"}
	}
	// Add newly observed evidence without replacing an established identifier.
	if c.expectedIdentity.serial == "" {
		c.expectedIdentity.serial = observed.serial
	}
	if c.expectedIdentity.mac == "" {
		c.expectedIdentity.mac = observed.mac
	}
	return nil
}

// observeIdentity is also used by DescribeDevice. Pairing info is public, so
// reading it must never send credentials or a cached token to an unchecked miner.
func (c *Client) observeIdentity(ctx context.Context) (pairingInfoResponse, error) {
	c.identityMu.Lock()
	defer c.identityMu.Unlock()
	return c.observeIdentityLocked(ctx)
}

func (c *Client) observeIdentityLocked(ctx context.Context) (pairingInfoResponse, error) {
	var info pairingInfoResponse
	resp, err := c.sendUncheckedRequest(ctx, http.MethodGet, pairingInfoPath, nil, "", nil)
	if err == nil {
		_, err = decodeGetResponse(resp, &info)
	}
	if err != nil {
		if c.expectedIdentity != nil {
			// Do not classify an unavailable public identity endpoint as a rejected
			// miner password, even if its response happens to be HTTP 401/403.
			err = &identityError{reason: "identity endpoint is unavailable or returned an invalid response"}
		}
		return pairingInfoResponse{}, err
	}
	if c.expectedIdentity != nil {
		if err := c.acceptIdentityLocked(newDeviceIdentity(info.CbSn, info.Mac)); err != nil {
			return pairingInfoResponse{}, err
		}
	}
	return info, nil
}

// verifyIdentity uses the public pairing endpoint directly: using doGet here
// would recurse through authentication and request validation. One small GET
// per live API request also detects replacement when the cached token still works.
// MAC/serial are endpoint checks on the trusted miner LAN, not cryptographic proof.
func (c *Client) verifyIdentity(ctx context.Context) error {
	c.identityMu.Lock()
	defer c.identityMu.Unlock()
	if c.expectedIdentity == nil {
		return nil
	}
	_, err := c.observeIdentityLocked(ctx)
	return err
}
