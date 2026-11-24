package mqttingest

import (
	"context"
	"fmt"

	"golang.org/x/sync/semaphore"
)

type sourceMutationKey struct {
	organizationID int64
	sourceID       int64
}

type sourceMutationLock struct {
	semaphore *semaphore.Weighted
	refs      int
}

// acquireSourceMutation keeps same-source writers out of the quiesce/write/reload
// interval. Different sources can still save settings independently.
func (s *SettingsService) acquireSourceMutation(ctx context.Context, orgID, sourceID int64) (func(), error) {
	key := sourceMutationKey{organizationID: orgID, sourceID: sourceID}
	s.sourceMutationMu.Lock()
	if s.sourceMutations == nil {
		s.sourceMutations = make(map[sourceMutationKey]*sourceMutationLock)
	}
	lock := s.sourceMutations[key]
	if lock == nil {
		lock = &sourceMutationLock{semaphore: semaphore.NewWeighted(1)}
		s.sourceMutations[key] = lock
	}
	lock.refs++
	s.sourceMutationMu.Unlock()

	releaseRef := func() {
		s.sourceMutationMu.Lock()
		defer s.sourceMutationMu.Unlock()
		lock.refs--
		if lock.refs == 0 {
			delete(s.sourceMutations, key)
		}
	}
	if !lock.semaphore.TryAcquire(1) {
		if err := lock.semaphore.Acquire(ctx, 1); err != nil {
			releaseRef()
			return nil, fmt.Errorf("mqttingest: wait for source settings mutation: %w", err)
		}
	}
	return func() {
		lock.semaphore.Release(1)
		releaseRef()
	}, nil
}
