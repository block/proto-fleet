package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ullaakut/nmap/v3"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
)

const (
	nmapScanTimeout       = 600 * time.Second
	nmapHostTimeoutMs     = 10000
	nmapMinRTTMs          = 100
	nmapProbeConcurrency  = 16
	nmapDefaultBinaryName = "nmap"
)

// resolveNmapPath returns either an absolute path (explicit flag or
// binary-adjacent default) or the bare name "nmap" so Ullaakut's exec.LookPath
// resolves from PATH at scan time. Bare name is a deliberate contract with the
// library, not an oversight.
func resolveNmapPath(flag, exeDir string) (string, error) {
	if flag != "" {
		if !filepath.IsAbs(flag) {
			return "", fmt.Errorf("--nmap-path must be an absolute path, got %q", flag)
		}
		if err := checkExecutableFile(flag); err != nil {
			return "", fmt.Errorf("--nmap-path %s: %w", flag, err)
		}
		return flag, nil
	}
	if exeDir != "" {
		candidate := filepath.Join(exeDir, nmapDefaultBinaryName)
		if err := checkExecutableFile(candidate); err == nil {
			return candidate, nil
		}
	}
	return nmapDefaultBinaryName, nil
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

func (r *RunCmd) runNmapDiscovery(ctx context.Context, req *pairingpb.NmapModeRequest, logger *slog.Logger) ([]*pb.DiscoveredDeviceReport, error) {
	target := strings.TrimSpace(req.GetTarget())
	if target == "" {
		return nil, errors.New("nmap target is required")
	}
	ports := req.GetPorts()
	if len(ports) == 0 {
		ports = r.discoverer.DefaultDiscoveryPorts(ctx)
	}
	if len(ports) == 0 {
		return nil, errors.New("no ports to scan; pass ports or load discovery plugins")
	}

	scanCtx, cancel := context.WithTimeout(ctx, nmapScanTimeout)
	defer cancel()

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
