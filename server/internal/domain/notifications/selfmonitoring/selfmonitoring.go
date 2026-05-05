// Package selfmonitoring exposes the names and guard helpers for ProtoFleet's
// built-in monitoring-of-the-monitoring-stack.
//
// The vmalert rule group "protofleet-self" (file:
// deployment-files/server/monitoring/vmalert/rules.d/protofleet-self.yml)
// always loads, independent of any user-defined rules, and routes through
// the "__protofleet_internal" Alertmanager receiver. Both names are
// protected: the future rules and channels APIs (Epic D) call GuardDelete /
// GuardMutate before any DELETE/PUT, ensuring an operator cannot remove the
// fleet's only signal that the monitoring pipeline itself has failed.
package selfmonitoring

import "errors"

// SelfMonitoringRuleGroup is the protected vmalert rule group containing
// alerts for the monitoring stack itself (collector outage, evaluator
// stalled, alertmanager unreachable, monitoring-stack degraded).
const SelfMonitoringRuleGroup = "protofleet-self"

// InternalReceiver is the protected Alertmanager receiver used to route
// self-monitoring alerts back into ProtoFleet's activity log via the
// /internal/alertmanager-webhook endpoint.
const InternalReceiver = "__protofleet_internal"

// ErrProtectedGroup is returned when a caller attempts to delete or mutate
// a protected rule group.
var ErrProtectedGroup = errors.New("rule group is protected and cannot be modified or deleted")

// ErrProtectedReceiver is returned when a caller attempts to delete or
// rename a protected Alertmanager receiver.
var ErrProtectedReceiver = errors.New("alertmanager receiver is protected and cannot be modified or deleted")

// IsProtectedGroup reports whether name is a protected vmalert rule group.
func IsProtectedGroup(name string) bool {
	return name == SelfMonitoringRuleGroup
}

// IsProtectedReceiver reports whether name is a protected Alertmanager
// receiver.
func IsProtectedReceiver(name string) bool {
	return name == InternalReceiver
}

// GuardDelete returns ErrProtectedGroup if name is a protected rule group.
// Rules-API DELETE handlers MUST call this before deletion.
func GuardDelete(name string) error {
	if IsProtectedGroup(name) {
		return ErrProtectedGroup
	}
	return nil
}

// GuardReceiverDelete returns ErrProtectedReceiver if name is a protected
// receiver. Channels-API DELETE handlers MUST call this before deletion.
func GuardReceiverDelete(name string) error {
	if IsProtectedReceiver(name) {
		return ErrProtectedReceiver
	}
	return nil
}
