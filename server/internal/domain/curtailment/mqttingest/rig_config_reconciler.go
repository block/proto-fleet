package mqttingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/authn"

	"github.com/block/proto-fleet/server/internal/domain/session"
)

const maxRigConfigReconciliationsPerPass = 100

// Start activates durable rig-config delivery. Settings writes commit a new
// generation before waking this loop, so a crash or enqueue failure leaves the
// work available to this or another fleetd instance.
func (s *SettingsService) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start rig config reconciler: %w", err)
	}
	if s.rigConfigApplier == nil {
		return errors.New("start rig config reconciler: RigConfigApplier is required")
	}
	if s.rigConfigStore == nil {
		return errors.New("start rig config reconciler: RigConfigStore is required")
	}

	s.rigConfigRunMu.Lock()
	defer s.rigConfigRunMu.Unlock()
	if s.rigConfigCancel != nil {
		return errors.New("rig config reconciler already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.rigConfigCancel = cancel
	s.rigConfigDone = done
	go func() {
		defer close(done)
		s.runRigConfigReconciler(runCtx)
	}()
	return nil
}

// Stop prevents new claims and waits for the active claim to finish.
func (s *SettingsService) Stop(ctx context.Context) error {
	s.rigConfigRunMu.Lock()
	cancel := s.rigConfigCancel
	done := s.rigConfigDone
	s.rigConfigRunMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		s.rigConfigRunMu.Lock()
		if s.rigConfigDone == done {
			s.rigConfigCancel = nil
			s.rigConfigDone = nil
		}
		s.rigConfigRunMu.Unlock()
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop rig config reconciler: %w", ctx.Err())
	}
}

func (s *SettingsService) runRigConfigReconciler(ctx context.Context) {
	ticker := time.NewTicker(rigConfigPollInterval)
	defer ticker.Stop()
	for {
		s.processDueRigConfigReconciliations(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.rigConfigWake:
		}
	}
}

func (s *SettingsService) processDueRigConfigReconciliations(ctx context.Context) {
	for range maxRigConfigReconciliationsPerPass {
		if err := ctx.Err(); err != nil {
			return
		}
		request, err := s.rigConfigStore.ClaimRigConfigReconciliation(ctx)
		if errors.Is(err, ErrRigConfigReconciliationNotFound) {
			return
		}
		if err != nil {
			slog.Error("claim Proto rig curtailment config reconciliation", "error", err)
			return
		}
		s.processRigConfigReconciliation(ctx, request)
	}
}

func (s *SettingsService) processRigConfigReconciliation(ctx context.Context, request RigConfigReconciliation) {
	applyCtx, cancel := context.WithTimeout(ctx, s.reconcileTimeout)
	defer cancel()
	applyCtx = authn.SetInfo(applyCtx, &session.Info{
		SessionID:      rigConfigReapplyActorName,
		UserID:         request.RequestedBy,
		OrganizationID: request.OrganizationID,
		ExternalUserID: rigConfigReapplyActorName,
		Username:       rigConfigReapplyActorName,
		Actor:          session.ActorCurtailment,
	})

	err := s.applyRigConfigReconciliation(applyCtx, request)
	if err != nil {
		slog.Error("enqueue Proto rig curtailment config reconciliation",
			"org_id", request.OrganizationID,
			"generation", request.DesiredGeneration,
			"attempt", request.DeliveryAttempts,
			"error", err,
		)
		if request.DeliveryAttempts == rigConfigFastDeliveryAttempts {
			slog.Error("Proto rig curtailment config delivery exhausted fast retries; retrying on a slow period until it succeeds",
				"org_id", request.OrganizationID,
				"generation", request.DesiredGeneration,
				"retry_interval", rigConfigSlowRetryInterval,
			)
		}
		s.retryRigConfigReconciliation(ctx, request, err)
		return
	}
	if err := s.rigConfigStore.CompleteRigConfigReconciliation(applyCtx, request.OrganizationID, request.DesiredGeneration); err != nil {
		slog.Error("complete Proto rig curtailment config reconciliation",
			"org_id", request.OrganizationID,
			"generation", request.DesiredGeneration,
			"error", err,
		)
	}
}

func (s *SettingsService) applyRigConfigReconciliation(ctx context.Context, request RigConfigReconciliation) error {
	// The full-delivery watermark belongs to the claimed snapshot. A later
	// settings write must not change the scope of this already claimed range.
	targeted := request.FullReconcileGeneration <= request.EnqueuedGeneration
	var identifiers []string
	var err error
	if targeted {
		identifiers, err = s.rigConfigStore.ListRigConfigReconciliationTargets(ctx, request.OrganizationID, request.EnqueuedGeneration, request.DesiredGeneration)
		if err != nil {
			return err
		}
		if len(identifiers) == 0 {
			// Devices may have been unpaired, deleted, or superseded by a newer
			// request. Completing this generation must never broaden its scope.
			return nil
		}
	}
	if scoped, ok := s.store.(RigConfigScopeStore); ok {
		scopes, err := scoped.ListRigConfigSourceScopes(ctx, request.OrganizationID, identifiers)
		if err != nil {
			return err
		}
		groups := make(map[string][]string)
		sourceGroups := make(map[string][]int64)
		for identifier, sourceIDs := range scopes {
			sort.Slice(sourceIDs, func(i, j int) bool { return sourceIDs[i] < sourceIDs[j] })
			parts := make([]string, len(sourceIDs))
			for i, id := range sourceIDs {
				parts[i] = strconv.FormatInt(id, 10)
			}
			key := strings.Join(parts, ",")
			groups[key] = append(groups[key], identifier)
			sourceGroups[key] = sourceIDs
		}
		keys := make([]string, 0, len(groups))
		for key := range groups {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			allowed := make(map[int64]bool)
			for _, id := range sourceGroups[key] {
				allowed[id] = true
			}
			config, err := s.buildScopedRigCurtailmentConfig(ctx, request.OrganizationID, allowed)
			if err != nil {
				return err
			}
			sort.Strings(groups[key])
			if err := s.rigConfigApplier.ApplyCurtailmentConfigToDevices(ctx, config, groups[key]); err != nil {
				return err
			}
		}
		return nil
	}
	return errors.New("rig config reconciliation requires a rule scope resolver")
}

func (s *SettingsService) retryRigConfigReconciliation(ctx context.Context, request RigConfigReconciliation, deliveryErr error) {
	retryCtx, cancel := context.WithTimeout(detachedContext(ctx), 5*time.Second)
	defer cancel()
	if err := s.rigConfigStore.RetryRigConfigReconciliation(
		retryCtx,
		request.OrganizationID,
		request.DesiredGeneration,
		deliveryErr.Error(),
	); err != nil {
		slog.Error("persist Proto rig curtailment config retry",
			"org_id", request.OrganizationID,
			"generation", request.DesiredGeneration,
			"error", err,
		)
	}
}

func (s *SettingsService) wakeRigConfigReconciler() {
	select {
	case s.rigConfigWake <- struct{}{}:
	default:
	}
}
