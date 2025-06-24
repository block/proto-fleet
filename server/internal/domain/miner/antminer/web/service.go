package web

import (
	"bytes"
	"context"
	"crypto/md5" // #nosec G501 - Required for digest authentication with Antminer devices
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

const (
	scheme               = "http"
	endpointSystemInfo   = "/cgi-bin/get_system_info.cgi"
	endpointMinerSummary = "/cgi-bin/summary.cgi"
	endpointMinerConfig  = "/cgi-bin/get_miner_conf.cgi"
	endpointNetworkInfo  = "/cgi-bin/get_network_info.cgi"
	endpointSetConfig    = "/cgi-bin/set_miner_conf.cgi"
	endpointReboot       = "/cgi-bin/reboot.cgi"
	endpointBlink        = "/cgi-bin/blink.cgi"
)

// BitmainWorkMode represents the operating mode of an Antminer device
type BitmainWorkMode string

// Bitmain work mode constants
const (
	BitmainWorkModeStart    BitmainWorkMode = "0" // Normal operation
	BitmainWorkModeSleep    BitmainWorkMode = "1" // Sleep mode
	BitmainWorkModeLowPower BitmainWorkMode = "2" // Low power mode
)

//go:generate mockgen -source=service.go -destination=mocks/mock_web_api_client.go -package=mocks WebAPIClient
type WebAPIClient interface {
	GetSystemInfo(ctx context.Context, connInfo *AntminerConnectionInfo) (*SystemInfo, error)
	GetMinerSummary(ctx context.Context, connInfo *AntminerConnectionInfo) (*MinerSummary, error)
	GetMinerConfig(ctx context.Context, connInfo *AntminerConnectionInfo) (*MinerConfig, error)
	GetNetworkInfo(ctx context.Context, connInfo *AntminerConnectionInfo) (*NetworkInfo, error)
	SetMinerConfig(ctx context.Context, connInfo *AntminerConnectionInfo, config *MinerConfig) error
	Reboot(ctx context.Context, connInfo *AntminerConnectionInfo) error
	StartBlink(ctx context.Context, connInfo *AntminerConnectionInfo) error
	StopBlink(ctx context.Context, connInfo *AntminerConnectionInfo) error
}

var _ WebAPIClient = &Service{}

type Service struct {
	httpClient *http.Client
}

func NewService() *Service {
	return &Service{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type SystemInfo struct {
	MinerType               string `json:"minertype"`
	NetType                 string `json:"nettype"`
	NetDevice               string `json:"netdevice"`
	MacAddr                 string `json:"macaddr"`
	Hostname                string `json:"hostname"`
	IPAddress               string `json:"ipaddress"`
	NetMask                 string `json:"netmask"`
	Gateway                 string `json:"gateway"`
	DNSServers              string `json:"dnsservers"`
	SystemMode              string `json:"system_mode"`
	SystemKernelVersion     string `json:"system_kernel_version"`
	SystemFilesystemVersion string `json:"system_filesystem_version"`
	FirmwareType            string `json:"firmware_type"`
	SerialNumber            string `json:"serinum"`
}

type MinerSummary struct {
	Status struct {
		Status     string `json:"STATUS"`
		When       int64  `json:"when"`
		Msg        string `json:"Msg"`
		APIVersion string `json:"api_version"`
	} `json:"STATUS"`
	Info struct {
		MinerVersion string `json:"miner_version"`
		CompileTime  string `json:"CompileTime"`
		Type         string `json:"type"`
	} `json:"INFO"`
	Summary []struct {
		Elapsed   int     `json:"elapsed"`
		Rate5s    float64 `json:"rate_5s"`
		Rate30m   float64 `json:"rate_30m"`
		RateAvg   float64 `json:"rate_avg"`
		RateIdeal float64 `json:"rate_ideal"`
		RateUnit  string  `json:"rate_unit"`
		HwAll     int     `json:"hw_all"`
		BestShare int64   `json:"bestshare"`
		Status    []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
			Code   int    `json:"code"`
			Msg    string `json:"msg"`
		} `json:"status"`
	} `json:"SUMMARY"`
}

type MinerConfig struct {
	Pools []struct {
		URL  string `json:"url"`
		User string `json:"user"`
		Pass string `json:"pass"`
	} `json:"pools"`
	APIListen              bool            `json:"api-listen"`
	APINetwork             bool            `json:"api-network"`
	APIGroups              string          `json:"api-groups"`
	APIAllow               string          `json:"api-allow"`
	BitmainFanCtrl         bool            `json:"bitmain-fan-ctrl"`
	BitmainFanPWM          string          `json:"bitmain-fan-pwm"`
	BitmainUseVil          bool            `json:"bitmain-use-vil"`
	BitmainFreq            string          `json:"bitmain-freq"`
	BitmainVoltage         string          `json:"bitmain-voltage"`
	BitmainCCDelay         string          `json:"bitmain-ccdelay"`
	BitmainPWTH            string          `json:"bitmain-pwth"`
	BitmainWorkMode        BitmainWorkMode `json:"bitmain-work-mode"`
	BitmainHashratePercent string          `json:"bitmain-hashrate-percent"`
	BitmainFreqLevel       string          `json:"bitmain-freq-level"`
}

type NetworkInfo struct {
	NetType        string `json:"nettype"`
	NetDevice      string `json:"netdevice"`
	MacAddr        string `json:"macaddr"`
	IPAddress      string `json:"ipaddress"`
	NetMask        string `json:"netmask"`
	ConfNetType    string `json:"conf_nettype"`
	ConfHostname   string `json:"conf_hostname"`
	ConfIPAddress  string `json:"conf_ipaddress"`
	ConfNetMask    string `json:"conf_netmask"`
	ConfGateway    string `json:"conf_gateway"`
	ConfDNSServers string `json:"conf_dnsservers"`
}

type RequestOptions struct {
	Method      string
	Endpoint    string
	Body        interface{}
	Result      interface{}
	ContentType string
}

func (s *Service) buildURL(connInfo *AntminerConnectionInfo, endpoint string) string {
	return connInfo.GetURL().JoinPath(endpoint).String()
}

func (s *Service) request(ctx context.Context, connInfo *AntminerConnectionInfo, opts RequestOptions) error {
	reqURL := s.buildURL(connInfo, opts.Endpoint)

	var bodyReader io.Reader
	if opts.Body != nil {
		bodyBuf := &bytes.Buffer{}
		encoder := json.NewEncoder(bodyBuf)
		if err := encoder.Encode(opts.Body); err != nil {
			return fleeterror.NewInternalErrorf("failed to encode request body: %v", err)
		}
		bodyReader = bodyBuf
	}

	req, err := http.NewRequestWithContext(ctx, opts.Method, reqURL, bodyReader)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to create request: %v", err)
	}

	if bodyReader != nil {
		if opts.ContentType != "" {
			req.Header.Set("Content-Type", opts.ContentType)
		} else {
			req.Header.Set("Content-Type", "application/json")
		}
	}

	if connInfo.Username != "" && connInfo.Password.Value() != "" {
		if err := s.addDigestAuth(req, connInfo.Username, connInfo.Password); err != nil {
			return fleeterror.NewInternalErrorf("failed to add digest auth: %v", err)
		}
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fleeterror.NewInternalErrorf("unexpected status code: %d", resp.StatusCode)
	}

	if opts.Result != nil {
		decoder := json.NewDecoder(resp.Body)
		if err := decoder.Decode(opts.Result); err != nil {
			return fleeterror.NewInternalErrorf("failed to decode response: %v", err)
		}
	}

	return nil
}

func (s *Service) GetSystemInfo(ctx context.Context, connInfo *AntminerConnectionInfo) (*SystemInfo, error) {
	var systemInfo SystemInfo
	err := s.request(ctx, connInfo, RequestOptions{
		Method:   http.MethodGet,
		Endpoint: endpointSystemInfo,
		Result:   &systemInfo,
	})
	if err != nil {
		return nil, err
	}
	return &systemInfo, nil
}

func (s *Service) GetMinerSummary(ctx context.Context, connInfo *AntminerConnectionInfo) (*MinerSummary, error) {
	var summary MinerSummary
	err := s.request(ctx, connInfo, RequestOptions{
		Method:   http.MethodGet,
		Endpoint: endpointMinerSummary,
		Result:   &summary,
	})
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

func (s *Service) GetMinerConfig(ctx context.Context, connInfo *AntminerConnectionInfo) (*MinerConfig, error) {
	var config MinerConfig
	err := s.request(ctx, connInfo, RequestOptions{
		Method:   http.MethodGet,
		Endpoint: endpointMinerConfig,
		Result:   &config,
	})
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (s *Service) GetNetworkInfo(ctx context.Context, connInfo *AntminerConnectionInfo) (*NetworkInfo, error) {
	var networkInfo NetworkInfo
	err := s.request(ctx, connInfo, RequestOptions{
		Method:   http.MethodGet,
		Endpoint: endpointNetworkInfo,
		Result:   &networkInfo,
	})
	if err != nil {
		return nil, err
	}
	return &networkInfo, nil
}

func (s *Service) SetMinerConfig(ctx context.Context, connInfo *AntminerConnectionInfo, config *MinerConfig) error {
	return s.request(ctx, connInfo, RequestOptions{
		Method:   http.MethodPost,
		Endpoint: endpointSetConfig,
		Body:     config,
	})
}

func (s *Service) Reboot(ctx context.Context, connInfo *AntminerConnectionInfo) error {
	return s.request(ctx, connInfo, RequestOptions{
		Method:   http.MethodPost,
		Endpoint: endpointReboot,
	})
}

func (s *Service) StartBlink(ctx context.Context, connInfo *AntminerConnectionInfo) error {
	return s.setBlink(ctx, connInfo, true)
}

func (s *Service) StopBlink(ctx context.Context, connInfo *AntminerConnectionInfo) error {
	return s.setBlink(ctx, connInfo, false)
}

func (s *Service) setBlink(ctx context.Context, connInfo *AntminerConnectionInfo, blink bool) error {
	blinkData := map[string]string{
		"blink": fmt.Sprintf("%t", blink),
	}

	return s.request(ctx, connInfo, RequestOptions{
		Method:   http.MethodPost,
		Endpoint: endpointBlink,
		Body:     blinkData,
	})
}

func (s *Service) addDigestAuth(req *http.Request, username string, password secrets.Text) error {
	challengeReq, err := http.NewRequestWithContext(req.Context(), req.Method, req.URL.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create challenge request: %v", err)
	}

	challengeResp, err := s.httpClient.Do(challengeReq)
	if err != nil {
		return fmt.Errorf("failed to get auth challenge: %v", err)
	}
	defer challengeResp.Body.Close()

	if challengeResp.StatusCode != http.StatusUnauthorized {
		return nil
	}

	authHeader := challengeResp.Header.Get("WWW-Authenticate")
	if authHeader == "" {
		return fmt.Errorf("no WWW-Authenticate header found")
	}

	challenge, err := parseDigestChallenge(authHeader)
	if err != nil {
		return fmt.Errorf("failed to parse digest challenge: %v", err)
	}

	digestAuth, err := generateDigestResponse(challenge, username, password, req.Method, req.URL.Path)
	if err != nil {
		return fmt.Errorf("failed to generate digest response: %v", err)
	}

	authHeaderValue := buildAuthorizationHeader(digestAuth)
	req.Header.Set("Authorization", authHeaderValue)

	return nil
}

func parseDigestChallenge(authHeader string) (*DigestChallenge, error) {
	if !strings.HasPrefix(strings.ToLower(authHeader), "digest ") {
		return nil, fmt.Errorf("not a digest authentication challenge")
	}

	params := strings.TrimPrefix(authHeader, "Digest ")
	params = strings.TrimPrefix(params, "digest ")

	challenge := &DigestChallenge{
		Algorithm: "MD5", // Default algorithm
	}

	paramRegex := regexp.MustCompile(`(\w+)=(?:"([^"]+)"|([^,\s]+))`)
	matches := paramRegex.FindAllStringSubmatch(params, -1)

	for _, match := range matches {
		key := strings.ToLower(match[1])
		value := match[2]
		if value == "" {
			value = match[3]
		}

		switch key {
		case "realm":
			challenge.Realm = value
		case "nonce":
			challenge.Nonce = value
		case "opaque":
			challenge.Opaque = value
		case "algorithm":
			challenge.Algorithm = value
		case "qop":
			challenge.QOP = value
		}
	}

	if challenge.Realm == "" || challenge.Nonce == "" {
		return nil, fmt.Errorf("missing required digest parameters")
	}

	return challenge, nil
}

func generateDigestResponse(challenge *DigestChallenge, username string, password secrets.Text, method, uri string) (*DigestAuth, error) {
	cnonce, err := generateCNonce()
	if err != nil {
		return nil, fmt.Errorf("failed to generate cnonce: %v", err)
	}

	nc := "00000001" // Nonce count

	auth := &DigestAuth{
		Username:  username,
		Password:  password,
		Realm:     challenge.Realm,
		Nonce:     challenge.Nonce,
		URI:       uri,
		Algorithm: challenge.Algorithm,
		Opaque:    challenge.Opaque,
		QOP:       challenge.QOP,
		NC:        nc,
		CNonce:    cnonce,
	}

	response := calculateDigestResponse(auth, method)
	auth.Response = response
	return auth, nil
}

func calculateDigestResponse(auth *DigestAuth, method string) string {
	ha1 := md5Hash(fmt.Sprintf("%s:%s:%s", auth.Username, auth.Realm, auth.Password.Value()))
	ha2 := md5Hash(fmt.Sprintf("%s:%s", method, auth.URI))

	var response string
	if auth.QOP == "auth" || auth.QOP == "auth-int" {
		response = md5Hash(fmt.Sprintf("%s:%s:%s:%s:%s:%s", ha1, auth.Nonce, auth.NC, auth.CNonce, auth.QOP, ha2))
	} else {
		response = md5Hash(fmt.Sprintf("%s:%s:%s", ha1, auth.Nonce, ha2))
	}

	return response
}

func buildAuthorizationHeader(auth *DigestAuth) string {
	var parts []string

	parts = append(parts, fmt.Sprintf(`username="%s"`, auth.Username))
	parts = append(parts, fmt.Sprintf(`realm="%s"`, auth.Realm))
	parts = append(parts, fmt.Sprintf(`nonce="%s"`, auth.Nonce))
	parts = append(parts, fmt.Sprintf(`uri="%s"`, auth.URI))
	parts = append(parts, fmt.Sprintf(`response="%s"`, auth.Response))

	if auth.Algorithm != "" {
		parts = append(parts, fmt.Sprintf(`algorithm=%s`, auth.Algorithm))
	}

	if auth.Opaque != "" {
		parts = append(parts, fmt.Sprintf(`opaque="%s"`, auth.Opaque))
	}

	if auth.QOP != "" {
		parts = append(parts, fmt.Sprintf(`qop=%s`, auth.QOP))
		parts = append(parts, fmt.Sprintf(`nc=%s`, auth.NC))
		parts = append(parts, fmt.Sprintf(`cnonce="%s"`, auth.CNonce))
	}

	return "Digest " + strings.Join(parts, ", ")
}

func generateCNonce() (string, error) {
	b := make([]byte, 16)
	n, err := rand.Read(b)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to generate random bytes: %v", err)
	}
	if n != len(b) {
		return "", fleeterror.NewInternalErrorf("failed to generate enough random bytes")
	}
	return hex.EncodeToString(b), nil
}

// md5Hash creates an MD5 hash of the input string
// #nosec G401 - MD5 is required for digest authentication with Antminer devices
func md5Hash(text string) string {
	hash := md5.Sum([]byte(text))
	return hex.EncodeToString(hash[:])
}
