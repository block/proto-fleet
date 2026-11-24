package pooldial

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"
)

func TestDestinationPolicy(t *testing.T) {
	for _, address := range []string{
		"127.0.0.1", "127.22.33.44", "10.1.2.3", "172.16.0.1", "172.31.255.254", "192.168.1.2",
		"169.254.169.254", "0.0.0.0", "0.1.2.3", "100.100.100.200", "198.18.0.1", "240.0.0.1", "255.255.255.255", "224.0.0.1",
		"::", "::1", "::ffff:127.0.0.1", "::ffff:10.1.2.3", "fc00::1", "fd00::1", "fe80::1", "fec0::1", "ff02::1",
	} {
		t.Run(address, func(t *testing.T) {
			require.ErrorIs(t, checkDestination(t.Context(), "tcp6", net.JoinHostPort(address, "3333"), nil), ErrPrivateDestination)
		})
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "172.32.0.1", "2606:4700:4700::1111", "::ffff:8.8.8.8"} {
		require.NoError(t, checkDestination(t.Context(), "tcp6", net.JoinHostPort(address, "3333"), nil))
	}
	for _, address := range []string{"pool.example:3333", "invalid", "[fe80::1%en0]:3333"} {
		require.ErrorIs(t, checkDestination(t.Context(), "tcp4", address, nil), ErrPrivateDestination)
	}
	require.ErrorIs(t, checkDestination(t.Context(), "unix", "/tmp/pool.sock", nil), ErrPrivateDestination)
	require.ErrorIs(t, checkDestination(t.Context(), "udp4", "8.8.8.8:3333", nil), ErrPrivateDestination)
	require.Nil(t, newDialer(t.Context()).ControlContext, "unrestricted callers retain private network access")
}

func TestPublicHostnameResolvingToLocalAddressesIsBlockedAtDial(t *testing.T) {
	// A public-looking hostname returns both local IPv4 and IPv6 addresses. Use
	// the actual Go resolver and dialer so neither candidate can bypass policy.
	dns, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		buffer := make([]byte, 1500)
		for {
			n, peer, err := dns.ReadFrom(buffer)
			if err != nil {
				return
			}
			var request dnsmessage.Message
			if request.Unpack(buffer[:n]) != nil {
				continue
			}
			response := dnsmessage.Message{Header: dnsmessage.Header{ID: request.ID, Response: true, Authoritative: true}, Questions: request.Questions}
			for _, question := range request.Questions {
				header := dnsmessage.ResourceHeader{Name: question.Name, Type: question.Type, Class: dnsmessage.ClassINET}
				if question.Type == dnsmessage.TypeA {
					response.Answers = append(response.Answers, dnsmessage.Resource{Header: header, Body: &dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}}})
				} else if question.Type == dnsmessage.TypeAAAA {
					response.Answers = append(response.Answers, dnsmessage.Resource{Header: header, Body: &dnsmessage.AAAAResource{AAAA: [16]byte{15: 1}}})
				}
			}
			encoded, err := response.Pack()
			if err == nil {
				_, _ = dns.WriteTo(encoded, peer)
			}
		}
	}()
	t.Cleanup(func() { _ = dns.Close(); <-finished })
	ctx, cancel := context.WithTimeout(PublicOnly(t.Context()), time.Second)
	defer cancel()
	dialer := newDialer(ctx)
	dialer.Resolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", dns.LocalAddr().String())
	}}
	conn, err := dialer.DialContext(ctx, "tcp", "pool.example.:3333")
	if conn != nil {
		_ = conn.Close()
	}
	require.ErrorIs(t, err, ErrPrivateDestination, "DNS results must be checked before any pool connection")
}
