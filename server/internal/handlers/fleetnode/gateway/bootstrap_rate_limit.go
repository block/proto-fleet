package gateway

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"connectrpc.com/connect"

	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

const (
	bootstrapRegistrationLimit = 120
	bootstrapHandshakeLimit    = 600
	bootstrapRateWindow        = time.Minute
	bootstrapRateMaxIPs        = 10000
)

type bootstrapRateWindowState struct {
	started time.Time
	count   int
}

type bootstrapRateBudget struct {
	mu         sync.Mutex
	limit      int
	entries    map[string]bootstrapRateWindowState
	overflow   bootstrapRateWindowState
	nextExpiry time.Time
	nextLog    time.Time
}

// BootstrapRateLimitOption creates process-local budgets shared by every RPC
// handler in this service. All attempts count, including failed authentication.
// Operational RPCs do not consume either bootstrap budget.
func BootstrapRateLimitOption() connect.HandlerOption {
	return bootstrapRateLimitOption(time.Now)
}

func bootstrapRateLimitOption(now func() time.Time) connect.HandlerOption {
	registration := &bootstrapRateBudget{limit: bootstrapRegistrationLimit}
	handshake := &bootstrapRateBudget{limit: bootstrapHandshakeLimit}
	return connect.WithRequestGate(func(ctx context.Context, spec connect.Spec, _ connect.Peer, _ http.Header) (context.Context, error) {
		var budget *bootstrapRateBudget
		switch spec.Procedure {
		case fleetnodegatewayv1connect.FleetNodeGatewayServiceRegisterProcedure:
			budget = registration
		case fleetnodegatewayv1connect.FleetNodeGatewayServiceBeginAuthHandshakeProcedure,
			fleetnodegatewayv1connect.FleetNodeGatewayServiceCompleteAuthHandshakeProcedure:
			budget = handshake
		default:
			return ctx, nil
		}
		// An unresolved address shares one bucket rather than bypassing admission.
		return ctx, budget.check(bootstrapRateKey(middleware.ClientIP(ctx)), now())
	})
}

// bootstrapRateKey counts IPv6 clients per /64 so a single host cannot dodge
// the quota by rotating addresses within its delegated prefix.
func bootstrapRateKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	switch {
	case addr.Is4In6():
		return addr.Unmap().String()
	case addr.Is6():
		return netip.PrefixFrom(addr, 64).Masked().String()
	default:
		return addr.String()
	}
}

func (b *bootstrapRateBudget) check(ip string, now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.entries == nil {
		b.entries = make(map[string]bootstrapRateWindowState)
	}
	entry, exists := b.entries[ip]
	if exists && now.Before(entry.started.Add(bootstrapRateWindow)) {
		if entry.count >= b.limit {
			return b.reject("ip_quota", entry.started.Add(bootstrapRateWindow).Sub(now), now)
		}
		entry.count++
		b.entries[ip] = entry
		return nil
	}
	if !exists && len(b.entries) >= bootstrapRateMaxIPs {
		// Prune only when full and an entry can have expired. Never evict live
		// counters, which would let IP churn reset someone else's quota.
		if !now.Before(b.nextExpiry) {
			b.nextExpiry = time.Time{}
			for key, state := range b.entries {
				expires := state.started.Add(bootstrapRateWindow)
				if !now.Before(expires) {
					delete(b.entries, key)
				} else if b.nextExpiry.IsZero() || expires.Before(b.nextExpiry) {
					b.nextExpiry = expires
				}
			}
		}
		if len(b.entries) >= bootstrapRateMaxIPs {
			return b.admitOverflow(now)
		}
	}
	b.entries[ip] = bootstrapRateWindowState{started: now, count: 1}
	if expires := now.Add(bootstrapRateWindow); b.nextExpiry.IsZero() || expires.Before(b.nextExpiry) {
		b.nextExpiry = expires
	}
	return nil
}

// admitOverflow runs under mu. Untracked IPs share one budget while the table
// is full, so IP churn cannot lock out new legitimate clients entirely.
func (b *bootstrapRateBudget) admitOverflow(now time.Time) error {
	expires := b.overflow.started.Add(bootstrapRateWindow)
	if !now.Before(expires) {
		b.overflow = bootstrapRateWindowState{started: now}
		expires = now.Add(bootstrapRateWindow)
	}
	if b.overflow.count >= b.limit {
		return b.reject("table_full", expires.Sub(now), now)
	}
	b.overflow.count++
	return nil
}

// reject runs under mu and logs at most once per budget per minute. Gate
// rejections bypass the usual logging interceptor; do not log IPs or credentials.
func (b *bootstrapRateBudget) reject(reason string, wait time.Duration, now time.Time) error {
	if !now.Before(b.nextLog) {
		slog.Warn("fleet node bootstrap throttled", "event", "fleet_node_bootstrap_throttled", "reason", reason, "requests_per_minute", b.limit)
		b.nextLog = now.Add(bootstrapRateWindow)
	}
	seconds := max(1, int((wait+time.Second-1)/time.Second))
	err := connect.NewError(connect.CodeResourceExhausted, errors.New("too many fleet node bootstrap requests; retry later"))
	err.Meta().Set("Retry-After", strconv.Itoa(seconds))
	return err
}
