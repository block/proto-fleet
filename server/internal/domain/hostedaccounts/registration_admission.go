package hostedaccounts

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Registration budgets are process-local and shared by all invitation services.
// Replicas each have these CPU budgets; public handlers separately apply the
// PostgreSQL client rate limit across preview and redemption paths. Small
// per-link/client allowances permit corrections while bounding reused links,
// rotating links, and aggregate bcrypt work. Requests never queue for a slot.
var sharedRegistrationAdmission = newRegistrationAdmission(registrationLimits{
	window: time.Minute, perInvitation: 3, perClient: 12, aggregate: 60,
	concurrent: 2, maxKeys: 4096,
}, time.Now)

type registrationLimits struct {
	window                              time.Duration
	perInvitation, perClient, aggregate int
	concurrent, maxKeys                 int
}

type registrationWindow struct {
	expires time.Time
	count   int
}

type registrationAdmission struct {
	mu          sync.Mutex
	limits      registrationLimits
	now         func() time.Time
	clients     map[[32]byte]registrationWindow
	invitations map[[32]byte]registrationWindow
	total       registrationWindow
	active      int
	nextPrune   time.Time
}

func newRegistrationAdmission(limits registrationLimits, now func() time.Time) *registrationAdmission {
	return &registrationAdmission{limits: limits, now: now,
		clients: make(map[[32]byte]registrationWindow), invitations: make(map[[32]byte]registrationWindow)}
}

func registrationBusy() error {
	return fleeterror.NewResourceExhaustedErrorf("registration temporarily limited; try again later")
}

// Internal callers may omit IP, but the public handler requires a trusted
// middleware address. Omission skips only this dimension, never hash budgets.
func (a *registrationAdmission) checkClient(ctx context.Context, ipAddress string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("registration client admission: %w", err)
	}
	if ipAddress == "" {
		return nil
	}
	ip, err := netip.ParseAddr(ipAddress)
	if err != nil {
		return fleeterror.NewInvalidArgumentError("invalid registration client address")
	}
	key := sha256.Sum256([]byte(ip.Unmap().String()))
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("registration client admission: %w", err)
	}
	now := a.now()
	a.prune(now)
	entry, exists := a.clients[key]
	if !exists && len(a.clients) >= a.limits.maxKeys {
		a.pruneExpired(now)
		if len(a.clients) >= a.limits.maxKeys {
			return registrationBusy()
		}
	}
	entry = a.current(entry, now)
	if entry.count >= a.limits.perClient {
		return registrationBusy()
	}
	entry.count++
	a.clients[key] = entry
	return nil
}

// Called only after authoritative Preview and cheap username availability.
// Bearer tokens and client addresses are never retained in admission state.
// Failed hashes/transactions retain rate credits, but always release the slot.
func (a *registrationAdmission) acquireHash(ctx context.Context, tokenHash string) (func(), error) {
	key := sha256.Sum256([]byte(tokenHash))
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("registration hash admission: %w", err)
	}
	now := a.now()
	a.prune(now)
	entry, exists := a.invitations[key]
	if !exists && len(a.invitations) >= a.limits.maxKeys {
		a.pruneExpired(now)
		if len(a.invitations) >= a.limits.maxKeys {
			return nil, registrationBusy()
		}
	}
	entry = a.current(entry, now)
	total := a.current(a.total, now)
	if entry.count >= a.limits.perInvitation || total.count >= a.limits.aggregate || a.active >= a.limits.concurrent {
		return nil, registrationBusy()
	}
	entry.count++
	total.count++
	a.invitations[key], a.total = entry, total
	a.active++
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			a.active--
		})
	}, nil
}

func (a *registrationAdmission) current(entry registrationWindow, now time.Time) registrationWindow {
	if !now.Before(entry.expires) {
		return registrationWindow{expires: now.Add(a.limits.window)}
	}
	return entry
}

func (a *registrationAdmission) prune(now time.Time) {
	if now.Before(a.nextPrune) {
		return
	}
	a.pruneExpired(now)
	a.nextPrune = now.Add(a.limits.window)
}

func (a *registrationAdmission) pruneExpired(now time.Time) {
	for key, entry := range a.clients {
		if !now.Before(entry.expires) {
			delete(a.clients, key)
		}
	}
	for key, entry := range a.invitations {
		if !now.Before(entry.expires) {
			delete(a.invitations, key)
		}
	}
	// Never evict a live key to admit a new one: rotation cannot erase budgets.
}
