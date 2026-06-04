package pairing

import (
	"sync"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
)

// dedupForwarder serializes concurrent Discover sources (the cloud scan and each
// fleet node) onto one server stream — Connect streams are not safe for
// concurrent Send — and dedupes devices across sources by pairing.DeviceDedupKey.
// The first Send error is recorded and onErr is invoked once so the caller can
// cancel the remaining sources.
type dedupForwarder struct {
	mu    sync.Mutex
	seen  map[string]struct{}
	send  func(*pb.DiscoverResponse) error
	onErr func()
	err   error
}

func newDedupForwarder(send func(*pb.DiscoverResponse) error, onErr func()) *dedupForwarder {
	return &dedupForwarder{seen: make(map[string]struct{}), send: send, onErr: onErr}
}

// forward dedupes resp's devices across sources and forwards it. A batch reduced
// entirely to duplicates (with no error payload) is dropped. Once any Send has
// failed, forward returns that error without sending again.
func (f *dedupForwarder) forward(resp *pb.DiscoverResponse) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	out := resp
	if len(resp.GetDevices()) > 0 {
		deduped := make([]*pb.Device, 0, len(resp.GetDevices()))
		for _, d := range resp.GetDevices() {
			key := pairing.DeviceDedupKey(d)
			if _, dup := f.seen[key]; dup {
				continue
			}
			f.seen[key] = struct{}{}
			deduped = append(deduped, d)
		}
		if len(deduped) == 0 && resp.GetError() == "" {
			return nil // whole batch was duplicates; nothing to forward
		}
		if len(deduped) < len(resp.GetDevices()) {
			out = &pb.DiscoverResponse{Devices: deduped, Error: resp.GetError()}
		}
	}
	if err := f.send(out); err != nil {
		f.err = err
		if f.onErr != nil {
			f.onErr()
		}
		return err
	}
	return nil
}

// failure returns the first Send error, if any.
func (f *dedupForwarder) failure() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}
