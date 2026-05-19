package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// resolveNmapPath picks the nmap binary path.
//
// Explicit flag: must be an absolute path that points at an executable file.
// No flag:       prefer <exeDir>/nmap (bundled-with-installer layout) when
//
//	it exists and is executable; otherwise return "nmap" so the
//	Ullaakut library + exec.LookPath resolve it from PATH at
//	scan time. Returning a name (not a path) here is the same
//	contract the underlying library uses internally.
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

// runNmapDiscovery executes an nmap scan against the request's target and
// ports, then runs the plugin Probe on every open host:port the scan
// reports. Returns reports the caller streams via ReportDiscoveredDevices.
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

	type hostPort struct{ ip, port string }
	var open []hostPort
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
			if p.Status() == "open" {
				open = append(open, hostPort{ip: ip, port: fmt.Sprintf("%d", p.ID)})
			}
		}
	}
	logger.Info("nmap scan complete", "open_endpoints", len(open))

	// Probe each open endpoint via the plugin manager. Mirror runProbes'
	// concurrency model so a flood of open hosts can't fork the plugin
	// manager into the ground.
	var (
		mu  sync.Mutex
		out []*pb.DiscoveredDeviceReport
		wg  sync.WaitGroup
	)
	sem := make(chan struct{}, nmapProbeConcurrency)
	for _, hp := range open {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return out, nil
		}
		wg.Add(1)
		go func(ip, port string) {
			defer wg.Done()
			defer func() { <-sem }()
			probeCtx, cancel := context.WithTimeout(ctx, perProbeTimeout)
			defer cancel()
			report, err := r.discoverer.Probe(probeCtx, ip, port)
			if err != nil {
				logger.Debug("nmap follow-up probe failed", "ip", ip, "port", port, "err", err)
				return
			}
			if report == nil || report.GetDeviceIdentifier() == "" {
				return
			}
			mu.Lock()
			out = append(out, report)
			mu.Unlock()
		}(hp.ip, hp.port)
	}
	wg.Wait()
	return out, nil
}
