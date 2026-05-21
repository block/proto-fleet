package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Ullaakut/nmap/v3"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/netutil"
)

// Targets reach nmap as argv (no shell), so the risk is nmap interpreting a
// leading dash as a flag (-iL, -oN -> arbitrary file read/write on the agent
// host). The range regex discriminates "A.B.C.D-N" from hostnames-with-dashes
// like "miner-01.lan" which the hostname grammar must still accept.
var (
	nmapHostnameRE  = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$`)
	nmapIPv4RangeRE = regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}-\d{1,3}$`)
	nmapAllowedRE   = regexp.MustCompile(`^[A-Za-z0-9.:/-]+$`)
)

func validateNmapTarget(s string) error {
	if s == "" {
		return errors.New("nmap target is required")
	}
	if strings.HasPrefix(s, "-") {
		return fmt.Errorf("nmap target %q must not start with '-'", s)
	}
	if !nmapAllowedRE.MatchString(s) {
		return fmt.Errorf("nmap target %q contains disallowed characters", s)
	}
	if _, err := netutil.ParseCIDROrIP(s); err == nil {
		return nil
	}
	if nmapIPv4RangeRE.MatchString(s) {
		head, tail, _ := strings.Cut(s, "-")
		ip := net.ParseIP(head)
		n, perr := strconv.Atoi(tail)
		if ip != nil && ip.To4() != nil && perr == nil && n >= 0 && n <= 255 {
			return nil
		}
		return fmt.Errorf("nmap target %q has invalid IPv4 range", s)
	}
	if nmapHostnameRE.MatchString(s) {
		return nil
	}
	return fmt.Errorf("nmap target %q is not a valid IP, CIDR, range, or hostname", s)
}

const (
	nmapScanTimeout       = 600 * time.Second
	nmapHostTimeoutMs     = 10000
	nmapMinRTTMs          = 100
	nmapProbeConcurrency  = 16
	nmapDefaultBinaryName = "nmap"
)

// PATH fallback keeps dev machines (brew install nmap) working without the
// installer-staged layout.
func resolveNmapPath(exeDir string) string {
	if exeDir != "" {
		candidate := filepath.Join(exeDir, nmapDefaultBinaryName)
		if err := checkExecutableFile(candidate); err == nil {
			return candidate
		}
	}
	return nmapDefaultBinaryName
}

func checkExecutableFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	if info.IsDir() {
		return errors.New("is a directory, not a file")
	}
	if info.Mode()&0o111 == 0 {
		return errors.New("not executable")
	}
	return nil
}

// detectIPv6Target mirrors pairing.validateNmapTargets: literal IPv6
// addresses and IPv6-only hostname resolutions need -6; IPv6 CIDR is
// rejected because nmap subnet scans don't make sense for 2^64 hosts.
func detectIPv6Target(ctx context.Context, target string) (useIPv6 bool, err error) {
	if _, ipNet, cerr := net.ParseCIDR(target); cerr == nil {
		if _, bits := ipNet.Mask.Size(); bits == 128 {
			return false, errors.New("IPv6 CIDR is not supported; use IpList for IPv6 devices")
		}
		return false, nil
	}
	if ip := net.ParseIP(target); ip != nil {
		return ip.To4() == nil, nil
	}
	addrs, lookupErr := net.DefaultResolver.LookupIPAddr(ctx, target)
	if lookupErr != nil || len(addrs) == 0 {
		// Fall through so nmap can try to resolve; matches pairing-service behavior.
		return false, nil //nolint:nilerr
	}
	for _, a := range addrs {
		if a.IP.To4() != nil {
			return false, nil
		}
	}
	return true, nil
}

func (r *RunCmd) buildNmapOptions(ctx context.Context, req *pairingpb.NmapModeRequest, ports []string) ([]nmap.Option, error) {
	target := strings.TrimSpace(req.GetTarget())
	if err := validateNmapTarget(target); err != nil {
		return nil, err
	}
	useIPv6, err := detectIPv6Target(ctx, target)
	if err != nil {
		return nil, err
	}
	if len(ports) == 0 {
		return nil, errors.New("no ports to scan; pass ports or load discovery plugins")
	}
	opts := []nmap.Option{
		nmap.WithBinaryPath(r.nmapPath),
		nmap.WithTargets(target),
		nmap.WithPorts(strings.Join(ports, ",")),
		nmap.WithUnique(),
		nmap.WithDisabledDNSResolution(),
		nmap.WithTimingTemplate(nmap.TimingAggressive),
		nmap.WithMaxRetries(1),
		nmap.WithHostTimeout(time.Duration(nmapHostTimeoutMs) * time.Millisecond),
		nmap.WithMinRTTTimeout(time.Duration(nmapMinRTTMs) * time.Millisecond),
	}
	if useIPv6 {
		opts = append(opts, nmap.WithIPv6Scanning())
	}
	return opts, nil
}

func (r *RunCmd) runNmapDiscovery(ctx context.Context, req *pairingpb.NmapModeRequest, ports []string, logger *slog.Logger) ([]*pb.DiscoveredDeviceReport, error) {
	opts, err := r.buildNmapOptions(ctx, req, ports)
	if err != nil {
		return nil, err
	}

	scanCtx, cancel := context.WithTimeout(ctx, nmapScanTimeout)
	defer cancel()

	scanner, err := nmap.NewScanner(scanCtx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create nmap scanner: %w", err)
	}
	result, _, err := scanner.Run()
	if err != nil {
		return nil, fmt.Errorf("nmap scan failed: %w", err)
	}
	if result == nil {
		return nil, nil
	}

	var open []endpoint
	for _, host := range result.Hosts {
		var ip string
		for _, a := range host.Addresses {
			if a.AddrType == "ipv4" || a.AddrType == "ipv6" {
				ip = a.Addr
				break
			}
		}
		if ip == "" {
			continue
		}
		for _, p := range host.Ports {
			if p.Status() == nmap.Open {
				open = append(open, endpoint{ip: ip, port: fmt.Sprintf("%d", p.ID)})
			}
		}
	}
	logger.Info("nmap scan complete", "open_endpoints", len(open))

	return fanOutProbes(ctx, open, nmapProbeConcurrency, r.discoverer.Probe, logger), nil
}
