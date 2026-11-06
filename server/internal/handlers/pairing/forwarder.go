package pairing

import (
	"context"
	"strings"
	"sync"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
)

// dedupForwarder dedupes concurrent Discover sources before preparing each
// accepted batch. It serializes only the stream sends, so independent pairing
// state lookups can run in parallel.
type dedupForwarder struct {
	stateMu          sync.Mutex
	sendMu           sync.Mutex
	seen             map[string]struct{}
	prepare          func(context.Context, []*pb.Device) error
	send             func(*pb.DiscoverResponse) error
	onErr            func()
	err              error
	stopped          bool
	inFlight         int
	limitWarningSent bool
	limitCancelSent  bool
}

func newDedupForwarder(
	prepare func(context.Context, []*pb.Device) error,
	send func(*pb.DiscoverResponse) error,
	onErr func(),
) *dedupForwarder {
	return &dedupForwarder{seen: make(map[string]struct{}), prepare: prepare, send: send, onErr: onErr}
}

// forward is the only path to the stream. It dedupes before prepare so duplicate
// devices do not cause database work. A prepare failure never sends an
// unannotated batch.
func (f *dedupForwarder) forward(ctx context.Context, resp *pb.DiscoverResponse) error {
	return f.forwardResponse(ctx, resp, false)
}

func (f *dedupForwarder) forwardTerminalWarning(ctx context.Context, warning string) error {
	return f.forwardResponse(ctx, &pb.DiscoverResponse{Warning: warning}, true)
}

func (f *dedupForwarder) forwardResponse(ctx context.Context, resp *pb.DiscoverResponse, terminal bool) error {
	f.stateMu.Lock()
	if f.err != nil {
		err := f.err
		f.stateMu.Unlock()
		return err
	}
	terminalWarningAfterStop := terminal && f.stopped && !f.limitWarningSent
	if f.stopped && !terminalWarningAfterStop {
		f.stateMu.Unlock()
		return nil
	}
	out := resp
	includesLimitWarning := false
	if len(resp.GetDevices()) > 0 {
		deduped := make([]*pb.Device, 0, min(len(resp.GetDevices()), discoverylimits.MaxUniqueDevicesPerRequest-len(f.seen)))
		for _, d := range resp.GetDevices() {
			key := pairing.DeviceDedupKey(d)
			if _, dup := f.seen[key]; dup {
				continue
			}
			if len(f.seen) == discoverylimits.MaxUniqueDevicesPerRequest {
				f.stopped = true
				break
			}
			f.seen[key] = struct{}{}
			deduped = append(deduped, d)
		}
		if len(deduped) == 0 && resp.GetWarning() == "" && !f.stopped {
			f.stateMu.Unlock()
			return nil // whole batch was duplicates; nothing to forward
		}
		if len(deduped) < len(resp.GetDevices()) {
			out = &pb.DiscoverResponse{Devices: deduped, Warning: resp.GetWarning()}
		}
	}
	if f.stopped && !terminalWarningAfterStop {
		out.Warning = strings.TrimSpace(out.GetWarning() + " " + discoverylimits.ResultLimitWarning)
		includesLimitWarning = true
	}
	f.inFlight++
	f.stateMu.Unlock()
	defer f.finishForward()

	if f.prepare != nil && len(out.GetDevices()) > 0 {
		if err := f.prepare(ctx, out.GetDevices()); err != nil {
			if ctx.Err() != nil {
				return err
			}
			return f.recordFailure(err)
		}
	}

	f.sendMu.Lock()
	defer f.sendMu.Unlock()
	if err := f.failure(); err != nil {
		return err
	}
	if err := f.send(out); err != nil {
		return f.recordFailure(err)
	}
	if includesLimitWarning {
		f.stateMu.Lock()
		f.limitWarningSent = true
		f.stateMu.Unlock()
	}
	return nil
}

func (f *dedupForwarder) finishForward() {
	f.stateMu.Lock()
	f.inFlight--
	cancelAtLimit := f.stopped && f.inFlight == 0 && f.err == nil && !f.limitCancelSent
	if cancelAtLimit {
		f.limitCancelSent = true
	}
	f.stateMu.Unlock()
	if cancelAtLimit && f.onErr != nil {
		f.onErr()
	}
}

func (f *dedupForwarder) recordFailure(err error) error {
	f.stateMu.Lock()
	if f.err == nil {
		f.err = err
	}
	recorded := f.err
	f.stateMu.Unlock()
	if f.onErr != nil {
		f.onErr()
	}
	return recorded
}

// failure returns the first prepare or Send error, if any.
func (f *dedupForwarder) failure() error {
	f.stateMu.Lock()
	defer f.stateMu.Unlock()
	return f.err
}
