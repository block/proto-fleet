package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Ullaakut/nmap/v3"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/netutil"
	"github.com/block/proto-fleet/server/internal/domain/nmaptarget"
)

// validateNmapTarget enforces the shared nmap target grammar (see nmaptarget).
func validateNmapTarget(s string) error {
	return nmaptarget.Validate(s)
}

const (
	nmapHostTimeout      = 10 * time.Second
	nmapMinRTT           = 100 * time.Millisecond
	nmapProbeConcurrency = 16
)

// nmapBinaryName and nmapAllowPATHFallback are platform-split.

// Adjacent <exe-dir>/nmap (installer-staged) takes precedence and fails
// closed if unsafe; we must not fall through to PATH because that would
// override the operator's staged binary. PATH fallback resolves once via
// LookPath, closing the resolve-vs-exec hijack window. Both candidates
// are chain-validated (writable parent dir lets a co-tenant swap the
// binary post-startup). Returns "" on any failure so scan-time errors
// cleanly instead of running an attacker-controlled binary.
func resolveNmapPath(exeDir string, logger *slog.Logger) string {
	if exeDir != "" {
		candidate := filepath.Join(exeDir, nmapBinaryName)
		target, err := validateNmapBinary(candidate)
		switch {
		case err == nil:
			if chainErr := validatePathChain(target); chainErr != nil {
				logger.Error("nmap exe-dir chain is unsafe; nmap-mode commands disabled", "path", target, "err", chainErr)
				return ""
			}
			return target
		case errors.Is(err, os.ErrNotExist):
			// Adjacent absent; fall through to PATH on platforms that allow it.
		default:
			logger.Error("nmap binary at exe-dir is unsafe; nmap-mode commands disabled", "path", candidate, "err", err)
			return ""
		}
	}
	if !nmapAllowPATHFallback {
		logger.Warn("no adjacent nmap binary and PATH fallback disabled on this platform; nmap-mode commands will fail")
		return ""
	}
	resolved, err := exec.LookPath(nmapBinaryName)
	if err != nil {
		logger.Warn("nmap not found on PATH; nmap-mode commands will fail", "err", err)
		return ""
	}
	target, vErr := validateNmapBinary(resolved)
	if vErr != nil {
		logger.Warn("nmap on PATH is unsafe; nmap-mode commands disabled", "path", resolved, "err", vErr)
		return ""
	}
	if chainErr := validatePathChain(target); chainErr != nil {
		logger.Warn("nmap PATH chain is unsafe; nmap-mode commands disabled", "path", target, "err", chainErr)
		return ""
	}
	return target
}

// Returns the resolved (post-symlink) path so a symlink swap after startup
// can't redirect exec to an unvalidated binary. Same safety bar as the
// plugin loader.
func validateNmapBinary(path string) (string, error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("eval symlinks: %w", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("stat: %w", err)
	}
	if info.IsDir() {
		return "", errors.New("is a directory, not a file")
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file (mode %s)", info.Mode())
	}
	if err := checkNmapBinaryOwnership(target, info); err != nil {
		return "", err
	}
	return target, nil
}

func (r *RunCmd) buildNmapOptions(ctx context.Context, req *pairingpb.NmapModeRequest, ports []string) ([]nmap.Option, error) {
	target := strings.TrimSpace(req.GetTarget())
	if err := validateNmapTarget(target); err != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "%s", err)
	}
	resolved, useIPv6, err := resolveNmapTarget(ctx, target, r.lookupIPAddr())
	if err != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "%s", err)
	}
	opts := []nmap.Option{
		nmap.WithBinaryPath(r.nmapPath),
		nmap.WithTargets(resolved),
		nmap.WithPorts(strings.Join(ports, ",")),
		nmap.WithUnique(),
		nmap.WithDisabledDNSResolution(),
		nmap.WithTimingTemplate(nmap.TimingAggressive),
		nmap.WithMaxRetries(1),
		nmap.WithHostTimeout(nmapHostTimeout),
		nmap.WithMinRTTTimeout(nmapMinRTT),
	}
	if useIPv6 {
		opts = append(opts, nmap.WithIPv6Scanning())
	}
	return opts, nil
}

// Mirrors pairing-service validateNmapTargets so agent and server feed
// nmap the same thing. IPv4 preferred for dual-stack hosts; literals,
// CIDRs, and IPv4 ranges pass through.
func resolveNmapTarget(ctx context.Context, target string, lookup func(context.Context, string) ([]net.IPAddr, error)) (string, bool, error) {
	if prefix, perr := netip.ParsePrefix(target); perr == nil {
		if prefix.Addr().Is6() {
			return "", false, errors.New("IPv6 CIDR is not supported; use IpList for IPv6 devices")
		}
		return target, false, nil
	}
	if addr, perr := netip.ParseAddr(target); perr == nil {
		return target, addr.Is6(), nil
	}
	if nmaptarget.IsIPv4Range(target) {
		return target, false, nil
	}
	addrs, lookupErr := lookup(ctx, target)
	if lookupErr != nil || len(addrs) == 0 {
		// Hand off to nmap's resolver on failure; matches pairing-service.
		return target, false, nil //nolint:nilerr
	}
	var ipv4, ipv6 string
	for _, a := range addrs {
		if a.IP.To4() != nil && ipv4 == "" {
			ipv4 = a.IP.String()
		} else if a.IP.To4() == nil && ipv6 == "" {
			ipv6 = a.IP.String()
		}
	}
	if ipv4 != "" {
		return ipv4, false, nil
	}
	if ipv6 != "" {
		return ipv6, true, nil
	}
	return target, false, nil
}

type ipResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

func (r *RunCmd) lookupIPAddr() func(context.Context, string) ([]net.IPAddr, error) {
	if r.resolver != nil {
		return r.resolver.LookupIPAddr
	}
	return net.DefaultResolver.LookupIPAddr
}

func (r *RunCmd) runNmapDiscovery(ctx context.Context, req *pairingpb.NmapModeRequest, ports []string, logger *slog.Logger) ([]*pb.DiscoveredDeviceReport, bool, error) {
	if r.nmapPath == "" {
		return nil, false, cmdErr(pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "nmap binary not available or unsafe; nmap-mode commands disabled")
	}
	opts, err := r.buildNmapOptions(ctx, req, ports)
	if err != nil {
		return nil, false, err
	}

	scanner, err := nmap.NewScanner(ctx, opts...)
	if err != nil {
		return nil, false, cmdErr(pb.AckCode_ACK_CODE_SCAN_FAILED, "create nmap scanner: %s", err)
	}
	result, _, err := scanner.Run()
	if err != nil {
		return nil, false, cmdErr(pb.AckCode_ACK_CODE_SCAN_FAILED, "nmap scan failed: %s", err)
	}
	if result == nil {
		return nil, false, nil
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
		// A misconfigured gateway often answers for the whole subnet; mirror
		// the server-side filter so it doesn't poison the batch.
		if addr, perr := netip.ParseAddr(ip); perr == nil && netutil.IsIPv4NetworkOrGateway(addr) {
			logger.Debug("skipping nmap result at network/gateway address", "ip", ip)
			continue
		}
		for _, p := range host.Ports {
			if p.Status() == nmap.Open {
				open = append(open, endpoint{ip: ip, port: strconv.Itoa(int(p.ID))})
			}
		}
	}
	logger.Info("nmap scan complete", "open_endpoints", len(open))

	reports, truncated := fanOutProbes(ctx, open, nmapProbeConcurrency, r.discoverer.Probe, logger)
	return reports, truncated, nil
}
