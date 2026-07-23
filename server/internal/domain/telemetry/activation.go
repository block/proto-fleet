package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/runtimejobs"
)

// telemetryActivation owns all work and queues for one active Fleet epoch.
// A later Start creates a new activation so results cannot cross epochs.
type telemetryActivation struct {
	cancel     context.CancelFunc
	stopping   <-chan struct{}
	stopped    chan struct{}
	background sync.WaitGroup

	tasks                chan models.Device
	statusTasks          chan models.Device
	results              telemetryResults
	statusFlushRequests  chan flushRequest
	metricsFlushRequests chan flushRequest

	producerMu         sync.Mutex
	acceptingProducers bool
	producerWG         sync.WaitGroup
}

func newTelemetryActivation(stopping <-chan struct{}, concurrencyLimit int) *telemetryActivation {
	return &telemetryActivation{
		stopping:    stopping,
		stopped:     make(chan struct{}),
		tasks:       make(chan models.Device, concurrencyLimit),
		statusTasks: make(chan models.Device, concurrencyLimit),
		results: telemetryResults{
			status:  make(chan statusResult, resultsChannelBuffer),
			metrics: make(chan metricsResult, resultsChannelBuffer),
		},
		statusFlushRequests:  make(chan flushRequest),
		metricsFlushRequests: make(chan flushRequest),
		acceptingProducers:   true,
	}
}

func (a *telemetryActivation) isStopping() bool {
	select {
	case <-a.stopping:
		return true
	default:
		return false
	}
}

func (a *telemetryActivation) registerProducer() (func(), bool) {
	a.producerMu.Lock()
	defer a.producerMu.Unlock()
	if !a.acceptingProducers {
		return nil, false
	}
	a.producerWG.Add(1)
	return a.producerWG.Done, true
}

func (a *telemetryActivation) drainProducers() {
	a.producerMu.Lock()
	a.acceptingProducers = false
	a.producerMu.Unlock()
	a.producerWG.Wait()
}

// Writers outlive activation cancellation long enough to consume the final
// results from every producer admitted before shutdown began.
func (a *telemetryActivation) writerContext(ctx context.Context) context.Context {
	writerCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	context.AfterFunc(ctx, func() {
		a.drainProducers()
		cancel()
	})
	return writerCtx
}

var _ runtimejobs.Lifecycle = (*TelemetryService)(nil)

func (s *TelemetryService) activeActivation() (*telemetryActivation, error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.activation == nil || s.activation.isStopping() {
		return nil, errTelemetryServiceInactive
	}
	return s.activation, nil
}

func (s *TelemetryService) registerActivationProducer() (*telemetryActivation, func(), error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.activation == nil || s.activation.isStopping() {
		return nil, nil, errTelemetryServiceInactive
	}
	release, ok := s.activation.registerProducer()
	if !ok {
		return nil, nil, errTelemetryServiceInactive
	}
	return s.activation, release, nil
}

// Start activates background telemetry collection for the lifetime of ctx.
func (s *TelemetryService) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start telemetry service: %w", err)
	}

	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.activation != nil {
		if s.activation.isStopping() {
			return fmt.Errorf("telemetry service is still stopping")
		}
		return nil
	}

	activationCtx, cancel := context.WithCancel(ctx)
	activation := newTelemetryActivation(activationCtx.Done(), s.config.ConcurrencyLimit)
	activation.cancel = cancel
	s.activation = activation

	for range s.config.ConcurrencyLimit {
		activation.producerWG.Go(func() { s.worker(activationCtx, activation) })
	}
	writerCtx := activation.writerContext(activationCtx)
	activation.background.Go(func() { s.gatherMetricsRoutine(activationCtx, activation.tasks) })
	activation.background.Go(func() { s.statusWriterRoutine(writerCtx, activation) })
	activation.background.Go(func() { s.metricsWriterRoutine(writerCtx, activation) })
	activation.background.Go(func() { s.devicePollingRoutine(activationCtx) })
	activation.background.Go(func() { s.statusPollingRoutine(activationCtx, activation.statusTasks) })
	activation.background.Go(func() { s.fleetStateSnapshotRoutine(activationCtx) })
	activation.background.Go(func() { s.fleetMetricRollupRoutine(activationCtx) })
	go s.finishActivation(activation)
	return nil
}

func (s *TelemetryService) finishActivation(activation *telemetryActivation) {
	activation.background.Wait()

	var pendingTelemetry []models.Device
drainTelemetryTasks:
	for {
		select {
		case device := <-activation.tasks:
			pendingTelemetry = append(pendingTelemetry, device)
		default:
			break drainTelemetryTasks
		}
	}
	s.requeueTelemetryTasks(pendingTelemetry)

drainStatusTasks:
	for {
		select {
		case device := <-activation.statusTasks:
			s.inFlight.Delete(device.ID)
		default:
			break drainStatusTasks
		}
	}

	s.lifecycleMu.Lock()
	if s.activation == activation {
		s.activation = nil
	}
	s.lifecycleMu.Unlock()
	close(activation.stopped)
}

func (s *TelemetryService) requeueTelemetryTasks(devices []models.Device) {
	if len(devices) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownFlushTimeout)
	defer cancel()
	if err := s.updateScheduler.AddDevices(ctx, devices...); err != nil {
		slog.Warn("failed to requeue telemetry tasks during shutdown", "count", len(devices), "error", err)
	}
}

func (s *TelemetryService) Stop(ctx context.Context) error {
	s.lifecycleMu.Lock()
	if s.activation == nil {
		s.lifecycleMu.Unlock()
		return nil
	}
	activation := s.activation
	activation.cancel()
	s.lifecycleMu.Unlock()

	select {
	case <-activation.stopped:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop telemetry service: %w", ctx.Err())
	}
}

// Close stops polling and closes per-organization broadcasters during process
// teardown. Runtime demotion should call Stop so read-only streams can remain
// independent of the active polling lifecycle.
func (s *TelemetryService) Close(ctx context.Context) error {
	stopErr := s.Stop(ctx)
	s.broadcasters.Range(func(key, value any) bool {
		if broadcaster, ok := value.(*TelemetryBroadcaster); ok {
			broadcaster.Stop()
		}
		s.broadcasters.Delete(key)
		return true
	})
	return stopErr
}
