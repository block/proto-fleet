// Package netscan provides shared target handling and bounded TCP discovery for
// Fleet Server and Fleet Nodes. Service identification remains with plugins.
package netscan

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net"
	"net/netip"
	"runtime"
	"slices"
	"sync"
	"syscall"
	"time"
)

const (
	scanConcurrency = 512
	connectTimeout  = 3 * time.Second
)

// Share the endpoint worker and socket budget across all scans, including scans
// on separate Scanner instances. Acquire capacity before starting each worker.
var dialPermits = make(chan struct{}, scanConcurrency)

// HostResult contains the ports that accepted a TCP connection. Scanning does
// not identify services or send application data.
type HostResult struct {
	Addr      netip.Addr
	OpenPorts []uint16
}

// Scanner provides bounded TCP connect scanning. It is safe for concurrent use.
type Scanner struct {
	dial func(context.Context, string, string) (net.Conn, error)
}

func NewScanner() *Scanner {
	return &Scanner{dial: (&net.Dialer{}).DialContext}
}

// Scan consumes addresses lazily and emits each host with at least one open
// port. Calls to emitHost are serialized and apply backpressure to scanning.
// The callback must return promptly when its own work is canceled.
//
// Cancellation and unexpected dial failures stop new work. Already discovered
// open ports are still emitted, including a host whose other ports could not be
// scanned. Scan returns the terminal error after all workers have stopped. If
// emitHost fails, no further callbacks are made and its error is returned.
func (s *Scanner) Scan(ctx context.Context, addrs iter.Seq[netip.Addr], ports []uint16, emitHost func(HostResult) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	if len(ports) == 0 {
		return nil
	}
	for _, port := range ports {
		if port == 0 {
			return errors.New("scan port must be nonzero")
		}
	}

	scanCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	results := make(chan HostResult, scanConcurrency)
	go func() {
		var workers sync.WaitGroup
		defer func() {
			workers.Wait()
			close(results)
		}()
		for addr := range addrs {
			if scanCtx.Err() != nil {
				return
			}
			if !addr.IsValid() {
				cancel(errors.New("cannot scan an invalid IP address"))
				return
			}
			host := &scanHost{addr: addr, results: results, remaining: len(ports)}
			for i, port := range ports {
				select {
				case dialPermits <- struct{}{}:
					workers.Go(func() {
						defer func() { <-dialPermits }()
						open, err := s.connect(scanCtx, host.addr, port)
						if err != nil {
							cancel(err)
						}
						if open {
							host.complete(port, 1)
						} else {
							host.complete(0, 1)
						}
					})
				case <-scanCtx.Done():
					host.complete(0, len(ports)-i)
					return
				}
			}
		}
	}()

	var callbackErr error
	for host := range results {
		if callbackErr == nil {
			callbackErr = emitHost(host)
			if callbackErr != nil {
				cancel(callbackErr)
			}
		}
	}
	if callbackErr != nil {
		return callbackErr
	}
	if err := context.Cause(scanCtx); err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	return nil
}

// A host only remains in memory while its active ports finish. The shared
// worker budget bounds host state, even for a very large range.
type scanHost struct {
	addr    netip.Addr
	results chan<- HostResult

	mu        sync.Mutex
	remaining int
	openPorts []uint16
}

// complete accounts for one probe, or all ports left unstarted on cancellation.
// A zero openPort records closed or unscanned ports.
func (h *scanHost) complete(openPort uint16, count int) {
	h.mu.Lock()
	if openPort != 0 {
		h.openPorts = append(h.openPorts, openPort)
	}
	h.remaining -= count
	finished := h.remaining == 0 && len(h.openPorts) != 0
	ports := h.openPorts
	h.mu.Unlock()
	if finished {
		slices.Sort(ports)
		h.results <- HostResult{Addr: h.addr, OpenPorts: ports}
	}
}

func (s *Scanner) connect(ctx context.Context, addr netip.Addr, port uint16) (bool, error) {
	if ctx.Err() != nil {
		return false, fmt.Errorf("connect canceled: %w", ctx.Err())
	}

	dialCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	endpoint := netip.AddrPortFrom(addr, port).String()
	conn, err := s.dial(dialCtx, "tcp", endpoint)
	cancel()
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		if conn == nil {
			return false, fmt.Errorf("connect %s: dial returned no connection", endpoint)
		}
		return true, nil
	}
	if ctx.Err() != nil {
		return false, fmt.Errorf("connect canceled: %w", ctx.Err())
	}
	if isClosedPortError(err) {
		return false, nil
	}
	return false, fmt.Errorf("connect %s: %w", endpoint, err)
}

func isClosedPortError(err error) bool {
	var networkErr net.Error
	if errors.As(err, &networkErr) && networkErr.Timeout() {
		return true
	}
	for _, closedErr := range []error{
		syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.ECONNABORTED,
		syscall.ENETUNREACH, syscall.EHOSTUNREACH, syscall.EHOSTDOWN,
	} {
		if errors.Is(err, closedErr) {
			return true
		}
	}
	if runtime.GOOS == "windows" {
		// Winsock errors do not match Go's POSIX-style syscall constants on
		// Windows. These values are the WSAE* socket error codes.
		const (
			wsaENetUnreach  syscall.Errno = 10051
			wsaEConnAborted syscall.Errno = 10053
			wsaEConnReset   syscall.Errno = 10054
			wsaETimedOut    syscall.Errno = 10060
			wsaEConnRefused syscall.Errno = 10061
			wsaEHostDown    syscall.Errno = 10064
			wsaEHostUnreach syscall.Errno = 10065
		)
		for _, closedErr := range []syscall.Errno{
			wsaENetUnreach, wsaEConnAborted, wsaEConnReset, wsaETimedOut,
			wsaEConnRefused, wsaEHostDown, wsaEHostUnreach,
		} {
			if errors.Is(err, closedErr) {
				return true
			}
		}
	}
	return false
}
