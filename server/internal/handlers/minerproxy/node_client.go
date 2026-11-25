package minerproxy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"buf.build/go/protovalidate"
	"golang.org/x/net/http/httpguts"
	"google.golang.org/protobuf/proto"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stableidentity"
)

// NodeClient reuses the direct proxy's HTTP authentication, path policy, and
// transport. It has no Fleet session or database access: the cloud authorizes
// and pins the route; the node opens its own encrypted credentials locally.
type NodeClient struct{ transport *Handler }

// NewNodeClient uses the normal network dialer when dialContext is nil.
// A custom dialer changes only connection establishment; request validation and
// identity verification still apply to every target.
func NewNodeClient(dialContext func(context.Context, string, string) (net.Conn, error)) *NodeClient {
	httpClient, httpsClient := newProxyHTTPClient(false), newProxyHTTPClient(true)
	if dialContext != nil {
		for _, client := range []*http.Client{httpClient, httpsClient} {
			if transport, ok := client.Transport.(*http.Transport); ok {
				transport.DialContext = dialContext
			}
		}
	}
	return &NodeClient{transport: &Handler{httpClient: httpClient, httpsClient: httpsClient, tokens: make(map[string]cachedToken)}}
}

func ValidateNodeWebRequest(request *pb.MinerWebRequest) error {
	if err := protovalidate.Validate(request); err != nil {
		return fleeterror.NewInvalidArgumentError("invalid miner web request")
	}
	target := request.GetTarget()
	if target.GetDriverName() != "proto" || target.GetDeviceClass() != commonpb.DeviceClass_DEVICE_CLASS_RIG {
		return fleeterror.NewInvalidArgumentError("embedded node views require a Proto rig")
	}
	if _, err := parseRoutableMinerAddr(target.GetIpAddress()); err != nil {
		return err
	}
	if target.GetUrlScheme() != "http" && target.GetUrlScheme() != "https" {
		return fleeterror.NewInvalidArgumentError("unsupported miner URL scheme")
	}
	if !stableidentity.New(target.GetSerialNumber(), target.GetMacAddress()).Usable() {
		return fleeterror.NewInvalidArgumentError("miner identity is required")
	}
	rest := strings.TrimPrefix(request.GetPath(), "/api/")
	version, endpoint, ok := strings.Cut(rest, "/")
	canonical, allowed := proxyPathFor(version, request.GetMethod(), endpoint)
	if !ok || !allowed || canonical != request.GetPath() || isUnproxyableEndpoint(request.GetMethod(), canonical) {
		return fleeterror.NewInvalidArgumentError("miner API path is not allowed")
	}
	// v2 is probed by ProtoOS before v1. Rig v2 controls have no supported UI yet.
	if version == "v2" && (endpoint != "system/info" || request.GetMethod() != http.MethodGet) {
		return fleeterror.NewInvalidArgumentError("unsupported rig API version")
	}
	if request.GetTimeout().AsDuration() > proxyClientTimeout {
		return fleeterror.NewInvalidArgumentError("miner web execution timeout exceeds limit")
	}
	if strings.ContainsAny(request.GetQuery(), "\r\n#") {
		return fleeterror.NewInvalidArgumentError("invalid miner query")
	}
	for _, h := range request.GetHeaders() {
		if !httpguts.ValidHeaderFieldName(h.GetName()) || !httpguts.ValidHeaderFieldValue(h.GetValue()) {
			return fleeterror.NewInvalidArgumentError("invalid miner headers")
		}
	}
	return nil
}

func (c *NodeClient) Forward(ctx context.Context, request *pb.MinerWebRequest, password func(context.Context) (string, error)) (*http.Response, error) {
	if err := ValidateNodeWebRequest(request); err != nil {
		return nil, err
	}
	// A local execution budget, independent of server clocks and delivery time.
	// WithTimeout also preserves an earlier node-handler/session deadline.
	ctx, cancel := context.WithTimeout(ctx, request.GetTimeout().AsDuration())
	failed := true
	defer func() {
		if failed {
			cancel()
		}
	}()
	descriptor := request.GetTarget()
	if len(descriptor.GetCredentialPassword()) > 0 && password == nil {
		return nil, fleeterror.NewFailedPreconditionError("node credential provider unavailable")
	}
	addr, _ := parseRoutableMinerAddr(descriptor.GetIpAddress())
	host, err := minerHost(addr, descriptor.GetPort())
	if err != nil {
		return nil, err
	}
	base := (&url.URL{Scheme: descriptor.GetUrlScheme(), Host: host}).String()
	binding, err := proto.Marshal(descriptor)
	if err != nil {
		return nil, fmt.Errorf("encode miner binding: %w", err)
	}
	target := proxyTarget{
		deviceIdentifier: descriptor.GetDeviceIdentifier(), baseURL: base,
		passwordEnc: sql.NullString{String: "node-local", Valid: len(descriptor.GetCredentialPassword()) > 0},
		password:    password, tokenKey: fmt.Sprintf("%x", sha256.Sum256(binding)),
	}
	verify := func(ctx context.Context) error { return c.verifyIdentity(ctx, base, descriptor) }
	// Verify before decrypting credentials, and again before each physical send.
	if err := verify(ctx); err != nil {
		return nil, err
	}
	target.dispatch = func(ctx context.Context, _ bool, call func(context.Context) error) error {
		if err := verify(ctx); err != nil {
			return err
		}
		return call(ctx)
	}
	source := &http.Request{Method: request.GetMethod(), URL: &url.URL{RawQuery: request.GetQuery()}, Header: make(http.Header)}
	for _, h := range request.GetHeaders() {
		if nodeRequestHeaderAllowed(h.GetName()) {
			source.Header.Add(h.GetName(), h.GetValue())
		}
	}
	response, err := c.transport.forwardAuthenticated(ctx, source, target, request.GetPath(), request.GetBody())
	if err != nil {
		return nil, err
	}
	response.Body = &cancelResponseBody{ReadCloser: response.Body, cancel: cancel}
	failed = false
	return response, nil
}

func (c *NodeClient) verifyIdentity(ctx context.Context, base string, target *pb.MinerConnectionDescriptor) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/pairing/info", nil)
	if err != nil {
		return fmt.Errorf("build identity request: %w", err)
	}
	response, err := c.transport.clientFor(base).Do(request)
	if err != nil {
		return fleeterror.NewUnavailableErrorf("miner identity check failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxLoginResponseBytes+1))
	if err != nil || len(body) > maxLoginResponseBytes || response.StatusCode != http.StatusOK {
		return fleeterror.NewUnavailableErrorf("miner identity check failed")
	}
	var identity struct {
		Serial string `json:"cb_sn"`
		MAC    string `json:"mac"`
	}
	if json.Unmarshal(body, &identity) != nil || !stableidentity.New(target.GetSerialNumber(), target.GetMacAddress()).Matches(stableidentity.New(identity.Serial, identity.MAC)) {
		return fleeterror.NewUnavailableErrorf("miner identity does not match the paired rig")
	}
	return nil
}

func nodeRequestHeaderAllowed(name string) bool {
	switch strings.ToLower(name) {
	case "content-type", "accept", "accept-encoding", "accept-language", "range", "if-range":
		return true
	default:
		return false
	}
}

// MinerWebResponseHeaders applies the proxy's header policy with the relay's
// bounded header count and values. Cookies and redirects never cross either path.
func MinerWebResponseHeaders(headers http.Header) []*pb.MinerWebHeader {
	var result []*pb.MinerWebHeader
	for name, relay := range responseHeaderPolicy {
		if value := headers.Get(name); relay && value != "" && len(value) <= 8192 {
			result = append(result, &pb.MinerWebHeader{Name: http.CanonicalHeaderKey(name), Value: value})
		}
	}
	return result
}

type cancelResponseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelResponseBody) Close() error {
	defer b.cancel()
	if err := b.ReadCloser.Close(); err != nil {
		return fmt.Errorf("close miner response: %w", err)
	}
	return nil
}
