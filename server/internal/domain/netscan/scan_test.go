package netscan

import (
	"context"
	"errors"
	"io"
	"iter"
	"net"
	"net/netip"
	"os"
	"runtime"
	"slices"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestScanLoopbackOpenConnectionIsClosed(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	openPort := netip.MustParseAddrPort(listener.Addr().String()).Port()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var hosts []HostResult
	err = NewScanner().Scan(ctx, scanAddrs("127.0.0.1"), []uint16{openPort}, func(host HostResult) error {
		hosts = append(hosts, host)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0].Addr.String() != "127.0.0.1" || !slices.Equal(hosts[0].OpenPorts, []uint16{openPort}) {
		t.Fatalf("unexpected hosts: %+v", hosts)
	}

	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if n, err := conn.Read(make([]byte, 1)); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("scanner connection was not closed: read = %d, %v", n, err)
	}
}

func TestScanSerializesCallbacksAndClosesConnections(t *testing.T) {
	var closed atomic.Int32
	scanner := NewScanner()
	scanner.dial = func(context.Context, string, string) (net.Conn, error) {
		return &scanTestConn{closed: &closed}, nil
	}
	var callbacks atomic.Int32
	var active atomic.Int32
	var concurrent atomic.Bool
	err := scanner.Scan(context.Background(), repeatedScanAddr(100), []uint16{443, 80}, func(host HostResult) error {
		if active.Add(1) != 1 {
			concurrent.Store(true)
		}
		defer active.Add(-1)
		callbacks.Add(1)
		if !slices.Equal(host.OpenPorts, []uint16{80, 443}) {
			t.Errorf("open ports = %v", host.OpenPorts)
		}
		runtime.Gosched()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if callbacks.Load() != 100 || closed.Load() != 200 || concurrent.Load() {
		t.Fatalf("callbacks=%d closed=%d concurrent=%v", callbacks.Load(), closed.Load(), concurrent.Load())
	}
}

func TestScanParallelPortsAndConnectDeadline(t *testing.T) {
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	var closed atomic.Int32
	scanner := NewScanner()
	scanner.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" {
			t.Errorf("network = %q", network)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 3*time.Second || time.Until(deadline) < 2*time.Second {
			t.Errorf("unexpected connect deadline: %v, %v", deadline, ok)
		}
		started <- struct{}{}
		select {
		case <-release:
			return &scanTestConn{closed: &closed}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		finished <- scanner.Scan(ctx, scanAddrs("::1"), []uint16{80, 443, 4028}, func(HostResult) error { return nil })
	}()
	for range 3 {
		scanReceive(t, started)
	}
	close(release)
	if err := scanReceive(t, finished); err != nil {
		t.Fatal(err)
	}
	if closed.Load() != 3 {
		t.Fatalf("closed = %d", closed.Load())
	}
}

func TestScanSharesDialLimitAcrossInstancesAndCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 1024)
	var active, maximum, exited, yielded atomic.Int32
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		current := active.Add(1)
		for old := maximum.Load(); old < current && !maximum.CompareAndSwap(old, current); old = maximum.Load() {
		}
		started <- struct{}{}
		<-ctx.Done()
		active.Add(-1)
		exited.Add(1)
		return nil, ctx.Err()
	}
	finished := make(chan error, 2)
	for range 2 {
		scanner := NewScanner()
		scanner.dial = dial
		go func() {
			addrs := func(yield func(netip.Addr) bool) {
				for range 1024 {
					yielded.Add(1)
					if !yield(netip.MustParseAddr("192.0.2.1")) {
						return
					}
				}
			}
			finished <- scanner.Scan(ctx, addrs, []uint16{80}, func(HostResult) error { return nil })
		}()
	}
	for range 512 {
		scanReceive(t, started)
	}
	select {
	case <-started:
		t.Fatal("more than 512 dials entered across concurrent scans")
	case <-time.After(20 * time.Millisecond):
	}
	// Each producer can fetch one next address while waiting for capacity.
	// Neither scan may create another pool of queued or blocked workers.
	if yielded.Load() > scanConcurrency+2 {
		t.Errorf("enumerated %d addresses with only %d worker permits", yielded.Load(), scanConcurrency)
	}
	cancel()
	for range 2 {
		if err := scanReceive(t, finished); !errors.Is(err, context.Canceled) {
			t.Fatalf("scan error = %v", err)
		}
	}
	if maximum.Load() != 512 || active.Load() != 0 || exited.Load() < 512 {
		t.Fatalf("maximum=%d active=%d exited=%d", maximum.Load(), active.Load(), exited.Load())
	}
}

func TestScanStopsLazyEnumerationAndDrainsWorkersOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 512)
	var yielded, exited atomic.Int32
	scanner := NewScanner()
	scanner.dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
		started <- struct{}{}
		<-ctx.Done()
		exited.Add(1)
		return nil, ctx.Err()
	}
	addrs := func(yield func(netip.Addr) bool) {
		for {
			yielded.Add(1)
			if !yield(netip.MustParseAddr("192.0.2.1")) {
				return
			}
		}
	}
	finished := make(chan error, 1)
	go func() { finished <- scanner.Scan(ctx, addrs, []uint16{80}, func(HostResult) error { return nil }) }()
	for range 512 {
		scanReceive(t, started)
	}
	cancel()
	if err := scanReceive(t, finished); !errors.Is(err, context.Canceled) {
		t.Fatalf("scan error = %v", err)
	}
	if yielded.Load() > scanConcurrency+1 || exited.Load() < 512 {
		t.Fatalf("yielded=%d exited=%d", yielded.Load(), exited.Load())
	}
}

func TestScanEmitsResultsBeforeEnumerationCompletes(t *testing.T) {
	emitted := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var closed atomic.Int32
	scanner := NewScanner()
	scanner.dial = func(context.Context, string, string) (net.Conn, error) {
		return &scanTestConn{closed: &closed}, nil
	}
	addrs := func(yield func(netip.Addr) bool) {
		if !yield(netip.MustParseAddr("192.0.2.1")) {
			return
		}
		select {
		case <-emitted:
		case <-ctx.Done():
		}
	}
	err := scanner.Scan(ctx, addrs, []uint16{80}, func(HostResult) error {
		close(emitted)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestScanPreservesResultsOnTerminalFailure(t *testing.T) {
	emitted := make(chan struct{})
	fatalErr := errors.New("dial failed unexpectedly")
	var closed atomic.Int32
	scanner := NewScanner()
	scanner.dial = func(ctx context.Context, _, address string) (net.Conn, error) {
		if address == "192.0.2.1:80" {
			return &scanTestConn{closed: &closed}, nil
		}
		select {
		case <-emitted:
			return nil, fatalErr
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	var hosts []HostResult
	err := scanner.Scan(context.Background(), scanAddrs("192.0.2.1", "192.0.2.2"), []uint16{80}, func(host HostResult) error {
		hosts = append(hosts, host)
		close(emitted)
		return nil
	})
	if !errors.Is(err, fatalErr) || len(hosts) != 1 || closed.Load() != 1 {
		t.Fatalf("err=%v hosts=%v closed=%d", err, hosts, closed.Load())
	}
}

func TestScanPreservesPartialHostOnTerminalFailure(t *testing.T) {
	open := make(chan struct{})
	fatalErr := errors.New("local socket failure")
	var closed atomic.Int32
	scanner := NewScanner()
	scanner.dial = func(ctx context.Context, _, address string) (net.Conn, error) {
		if address == "192.0.2.1:80" {
			close(open)
			return &scanTestConn{closed: &closed}, nil
		}
		select {
		case <-open:
			return nil, fatalErr
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	var hosts []HostResult
	err := scanner.Scan(context.Background(), scanAddrs("192.0.2.1"), []uint16{80, 443}, func(host HostResult) error {
		hosts = append(hosts, host)
		return nil
	})
	if !errors.Is(err, fatalErr) || len(hosts) != 1 || !slices.Equal(hosts[0].OpenPorts, []uint16{80}) || closed.Load() != 1 {
		t.Fatalf("err=%v hosts=%v closed=%d", err, hosts, closed.Load())
	}
}

func TestScanCallbackFailureCancelsAndDrains(t *testing.T) {
	callbackErr := errors.New("consumer stopped")
	var calls, closed, active atomic.Int32
	scanner := NewScanner()
	scanner.dial = func(ctx context.Context, _, address string) (net.Conn, error) {
		active.Add(1)
		defer active.Add(-1)
		if address == "192.0.2.1:80" {
			return &scanTestConn{closed: &closed}, nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	err := scanner.Scan(context.Background(), scanAddrs("192.0.2.1", "192.0.2.2"), []uint16{80}, func(HostResult) error {
		calls.Add(1)
		return callbackErr
	})
	if !errors.Is(err, callbackErr) || calls.Load() != 1 || active.Load() != 0 {
		t.Fatalf("err=%v calls=%d active=%d", err, calls.Load(), active.Load())
	}
}

func TestScanExpectedClosedPortsAreNotErrors(t *testing.T) {
	for _, dialErr := range []error{
		syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.ECONNABORTED,
		syscall.ENETUNREACH, syscall.EHOSTUNREACH, syscall.EHOSTDOWN,
		context.DeadlineExceeded,
	} {
		t.Run(dialErr.Error(), func(t *testing.T) {
			scanner := NewScanner()
			scanner.dial = func(context.Context, string, string) (net.Conn, error) {
				return nil, &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: dialErr}}
			}
			err := scanner.Scan(context.Background(), scanAddrs("192.0.2.1"), []uint16{80}, func(HostResult) error {
				t.Error("emitted a closed host")
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestScanResourceAndPermissionErrorsAreTerminal(t *testing.T) {
	for _, dialErr := range []error{
		syscall.EMFILE, syscall.ENFILE, syscall.ENOBUFS, syscall.ENOMEM,
		syscall.EACCES, syscall.EPERM, syscall.EADDRNOTAVAIL, syscall.ENETDOWN,
		errors.New("unexpected error"),
	} {
		t.Run(dialErr.Error(), func(t *testing.T) {
			scanner := NewScanner()
			scanner.dial = func(context.Context, string, string) (net.Conn, error) {
				return nil, &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: dialErr}}
			}
			err := scanner.Scan(context.Background(), scanAddrs("192.0.2.1"), []uint16{80}, func(HostResult) error { return nil })
			if !errors.Is(err, dialErr) {
				t.Fatalf("scan error = %v, want %v", err, dialErr)
			}
		})
	}
}

func TestScanWindowsClosedPortErrors(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Winsock error classification applies on Windows")
	}
	for _, errno := range []syscall.Errno{10051, 10053, 10054, 10060, 10061, 10064, 10065} {
		if !isClosedPortError(&net.OpError{Err: errno}) {
			t.Errorf("Winsock error %d was not recognized as a closed port", errno)
		}
	}
	for _, errno := range []syscall.Errno{10013, 10024, 10048, 10049, 10050, 10055} {
		if isClosedPortError(&net.OpError{Err: errno}) {
			t.Errorf("Winsock local error %d was recognized as a closed port", errno)
		}
	}
}

func BenchmarkScanSingleEndpoint(b *testing.B) {
	scanner := NewScanner()
	scanner.dial = func(context.Context, string, string) (net.Conn, error) {
		return nil, syscall.ECONNREFUSED
	}
	addrs := scanAddrs("192.0.2.1")
	ports := []uint16{80}
	b.ReportAllocs()
	for b.Loop() {
		if err := scanner.Scan(context.Background(), addrs, ports, func(HostResult) error { return nil }); err != nil {
			b.Fatal(err)
		}
	}
}

type scanTestConn struct {
	net.Conn
	closed *atomic.Int32
}

func (c *scanTestConn) Close() error {
	c.closed.Add(1)
	return nil
}

func scanAddrs(values ...string) iter.Seq[netip.Addr] {
	return func(yield func(netip.Addr) bool) {
		for _, value := range values {
			if !yield(netip.MustParseAddr(value)) {
				return
			}
		}
	}
}

func repeatedScanAddr(count int) iter.Seq[netip.Addr] {
	return func(yield func(netip.Addr) bool) {
		for range count {
			if !yield(netip.MustParseAddr("192.0.2.1")) {
				return
			}
		}
	}
}

func scanReceive[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %T", channel)
		var zero T
		return zero
	}
}
