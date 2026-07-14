package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestActiveHealthyRequiresUnexpiredLeaseAndRecentRenewal(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	snapshot := statusSnapshot{
		Active:         true,
		LeaseEpoch:     42,
		LeaseExpiresAt: now.Add(5 * time.Second),
		LastRenewAt:    now.Add(-1 * time.Second),
	}
	if !activeHealthy(snapshot, now, 6*time.Second) {
		t.Fatal("expected active lease to be healthy")
	}

	snapshot.LeaseExpiresAt = now.Add(-time.Millisecond)
	if activeHealthy(snapshot, now, 6*time.Second) {
		t.Fatal("expired lease must not be active healthy")
	}

	snapshot.LeaseExpiresAt = now.Add(5 * time.Second)
	snapshot.LastRenewAt = now.Add(-7 * time.Second)
	if activeHealthy(snapshot, now, 6*time.Second) {
		t.Fatal("stale renew timestamp must not be active healthy")
	}
}

func TestServeActiveFailsClosedWhenPassive(t *testing.T) {
	a := &app{
		cfg: config{
			HostID:    "fleet-a",
			LeaseName: defaultLeaseName,
			LeaseTTL:  defaultLeaseTTL,
		},
		holderID:  "fleet-a-test",
		startedAt: time.Now().UTC(),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/active", nil)
	a.serveActive(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected passive active-health failure, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"active":false`) {
		t.Fatalf("expected passive status body, got %s", rec.Body.String())
	}
}

func TestDSNHelpers(t *testing.T) {
	dsn := "postgres://fleet:secret@fleet-a:5432,fleet-b:5432/fleet?sslmode=disable&target_session_attrs=read-write"
	if !dsnLooksMultiHost(dsn) {
		t.Fatal("expected URL DSN to be detected as multi-host")
	}
	if !dsnHasReadWriteTarget(dsn) {
		t.Fatal("expected read-write target_session_attrs")
	}
	redacted := redactDSN(dsn)
	if strings.Contains(redacted, "secret") {
		t.Fatalf("redacted DSN leaked password: %s", redacted)
	}

	keyValueDSN := "host=fleet-a,fleet-b port=5432,5432 user=fleet password=secret dbname=fleet target_session_attrs=read-write"
	if !dsnLooksMultiHost(keyValueDSN) {
		t.Fatal("expected key/value DSN to be detected as multi-host")
	}
	redacted = redactDSN(keyValueDSN)
	if strings.Contains(redacted, "secret") {
		t.Fatalf("redacted key/value DSN leaked password: %s", redacted)
	}
}
