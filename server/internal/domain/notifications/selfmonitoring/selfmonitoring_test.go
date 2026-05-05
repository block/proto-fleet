package selfmonitoring

import (
	"errors"
	"testing"
)

func TestIsProtectedGroup(t *testing.T) {
	t.Parallel()

	if !IsProtectedGroup(SelfMonitoringRuleGroup) {
		t.Fatalf("expected %q to be protected", SelfMonitoringRuleGroup)
	}
	for _, name := range []string{"", "protofleet", "user-defined", "protofleet-self-extra"} {
		if IsProtectedGroup(name) {
			t.Errorf("expected %q to NOT be protected", name)
		}
	}
}

func TestIsProtectedReceiver(t *testing.T) {
	t.Parallel()

	if !IsProtectedReceiver(InternalReceiver) {
		t.Fatalf("expected %q to be protected", InternalReceiver)
	}
	for _, name := range []string{"", "default", "ops-pagerduty", "_protofleet_internal"} {
		if IsProtectedReceiver(name) {
			t.Errorf("expected %q to NOT be protected", name)
		}
	}
}

func TestGuardDelete_RejectsSelfGroup(t *testing.T) {
	t.Parallel()

	err := GuardDelete(SelfMonitoringRuleGroup)
	if !errors.Is(err, ErrProtectedGroup) {
		t.Fatalf("expected ErrProtectedGroup, got %v", err)
	}
}

func TestGuardDelete_AllowsUserGroup(t *testing.T) {
	t.Parallel()

	if err := GuardDelete("custom-org-rules"); err != nil {
		t.Fatalf("expected nil error for user group, got %v", err)
	}
}

func TestGuardReceiverDelete(t *testing.T) {
	t.Parallel()

	if err := GuardReceiverDelete(InternalReceiver); !errors.Is(err, ErrProtectedReceiver) {
		t.Fatalf("expected ErrProtectedReceiver, got %v", err)
	}
	if err := GuardReceiverDelete("ops-email"); err != nil {
		t.Fatalf("expected nil error for user receiver, got %v", err)
	}
}

// TestRulesAPIDoesNotDeleteProtectedGroup mirrors what a future
// RuleService.Delete handler will do. It ensures the guard is wired such
// that an attempted delete of the self-monitoring group is rejected before
// any storage call, while a legitimate user-group delete still reaches the
// storage path. When Epic D lands, replace this stub with a call to the
// real handler — the assertions stay the same.
func TestRulesAPIDoesNotDeleteProtectedGroup(t *testing.T) {
	t.Parallel()

	// Stand-in for the future RuleService.Delete handler. Records whether
	// the storage path was reached so each assertion can verify both the
	// returned error AND the side-effect (or lack thereof).
	var storageReached bool
	deleter := func(name string) error {
		if err := GuardDelete(name); err != nil {
			return err
		}
		// In the real handler this is where the storage call would happen.
		storageReached = true
		return nil
	}

	// Protected group: guard must short-circuit; storage must NOT be reached.
	storageReached = false
	if err := deleter(SelfMonitoringRuleGroup); !errors.Is(err, ErrProtectedGroup) {
		t.Fatalf("rules API allowed deletion of protected group: %v", err)
	}
	if storageReached {
		t.Fatalf("guard failed: storage path reached for protected group %q", SelfMonitoringRuleGroup)
	}

	// User group: guard must allow; storage path IS the legitimate path.
	storageReached = false
	if err := deleter("custom-rules"); err != nil {
		t.Fatalf("rules API rejected legitimate user group: %v", err)
	}
	if !storageReached {
		t.Fatalf("guard over-rejected: storage path not reached for user group %q", "custom-rules")
	}
}
