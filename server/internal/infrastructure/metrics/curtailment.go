package metrics

import "time"

// Counter samples store increments; dashboards sum their values over a window.
func (p *Provider) curtailmentIncrement(metric, kind string) {
	p.record(Sample{Metric: metric, Labels: Labels{Kind: kind}, Value: 1})
}

func (p *Provider) ObserveTickDuration(d time.Duration) {
	p.record(Sample{Metric: MetricCurtailmentTickDurationSeconds, Value: d.Seconds()})
}
func (p *Provider) IncTickFailure() { p.curtailmentIncrement(MetricCurtailmentTickFailureTotal, "") }
func (p *Provider) IncConfirmationPassFailure() {
	p.curtailmentIncrement(MetricCurtailmentConfirmationPassFailureTotal, "")
}
func (p *Provider) IncCandidateExcluded(reason string) {
	p.curtailmentIncrement(MetricCurtailmentCandidateExcludedTotal, reason)
}
func (p *Provider) IncMaintenanceOverride() {
	p.curtailmentIncrement(MetricCurtailmentMaintenanceOverrideTotal, "")
}
func (p *Provider) IncEventStateRaceLoss() {
	p.curtailmentIncrement(MetricCurtailmentEventStateRaceLossTotal, "")
}
func (p *Provider) IncTargetWriteFailure() {
	p.curtailmentIncrement(MetricCurtailmentTargetWriteFailureTotal, "")
}
func (p *Provider) IncAuditWriteFailure(activityType string) {
	p.curtailmentIncrement(MetricCurtailmentAuditWriteFailureTotal, activityType)
}
func (p *Provider) IncAllPairedPendingStall() {
	p.curtailmentIncrement(MetricCurtailmentAllPairedPendingStallTotal, "")
}
func (p *Provider) EmitCurtailmentPendingAges(eventAge, edgeAge time.Duration) {
	p.record(Sample{Metric: MetricCurtailmentOldestPendingEventAgeSeconds, Value: eventAge.Seconds()})
	p.record(Sample{Metric: MetricCurtailmentOldestPendingMQTTEdgeAgeSeconds, Value: edgeAge.Seconds()})
}
