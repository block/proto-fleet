package alerts

// Pre-migration compat: maintenance windows used to be stored as Grafana silences. Everything
// needed to recognize, migrate, and remove those silences lives here so it can be deleted as one
// unit once every deployment has run the startup sweep (the DeleteRule cleanup in user_rules.go
// also calls isLegacyMaintenanceWindowSilence).

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// The comment marker maintenance windows carried while they were stored as Grafana silences
// (before they moved to the database and delivery-side muting). Still recognized so the startup
// sweep and rule-deletion cleanup can find pre-migration silences.
const legacyMaintenanceWindowCommentMarker = "[proto-fleet-mw]"

func isLegacyMaintenanceWindowSilence(sil GrafanaSilence) bool {
	return strings.HasPrefix(sil.Comment, legacyMaintenanceWindowCommentMarker)
}

// MigrateLegacyMaintenanceWindowSilences copies the Grafana silences that backed maintenance
// windows before they moved to the database into alert_maintenance_window rows, then deletes
// them from Grafana. Without the delete, pre-migration windows would keep muting alerts at the
// Alertmanager — invisibly, since the window list no longer reads Grafana; without the copy,
// the delete would silently lift suppression mid-maintenance and flood operators. Runs across
// all orgs at startup. Each silence is deleted only after its replacement row is stored, and a
// re-run skips rows a previous partially-failed sweep already inserted, so retries converge.
// Live silences the DB model can't represent are left in place until they expire on their own.
func (s *Service) MigrateLegacyMaintenanceWindowSilences(ctx context.Context) (migrated, removed int, err error) {
	sils, err := s.grafana.ListSilences(ctx)
	if err != nil {
		return 0, 0, err
	}
	now := s.now()
	for _, sil := range sils {
		if !isLegacyMaintenanceWindowSilence(sil) {
			continue
		}
		// Expired silences are inert and Grafana garbage-collects them; deleting would just churn.
		if sil.Status != nil && sil.Status.State == "expired" {
			continue
		}
		// Only a still-live window has suppression to preserve; an already-ended one is just removed.
		if sil.EndsAt.After(now) {
			rec, ok := legacyWindowRecord(sil)
			if !ok {
				// Not representable in the rule×channel model (device-scoped silences from even older
				// builds, or foreign matcher shapes). Someone scheduled this suppression, so leave it
				// to run out: silences are finite and Grafana expires then garbage-collects them, so
				// the muting lifts itself and the sweep stops seeing it. Deleting now would flood
				// operators mid-maintenance instead.
				slog.Warn("alerts.legacy_window_retained_until_expiry",
					"silence_id", sil.ID, "comment", sil.Comment, "ends_at", sil.EndsAt)
				continue
			}
			// Re-list per silence rather than caching: the one-shot sweep is tiny, and a fresh
			// read naturally skips rows already inserted by an earlier partially-failed pass or
			// by an identical silence earlier in this one.
			orgWindows, err := s.windows.List(ctx, rec.OrganizationID)
			if err != nil {
				return migrated, removed, err
			}
			if !hasEquivalentWindow(orgWindows, rec) {
				if _, err := s.windows.Insert(ctx, rec); err != nil {
					return migrated, removed, err
				}
				migrated++
			}
		}
		if err := s.grafana.DeleteSilence(ctx, sil.ID); err != nil && !IsNotFound(err) {
			return migrated, removed, err
		}
		removed++
	}
	return migrated, removed, nil
}

// legacyWindowRecord maps a legacy silence onto the DB model. The old UI only created
// rule-scoped windows — exactly one org equality matcher and one rule-UID equality matcher —
// and a silence muted delivery everywhere, so the record targets that one rule across every
// channel. Any other shape reports !ok: an extra, regex, negated, or duplicate matcher narrows
// (or empties) what the silence mutes, so translating the recognizable subset would widen the
// replacement window beyond what the operator scheduled. Exactly-two plus the final validity
// check covers duplicates: a doubled matcher leaves no slot for the other required one.
func legacyWindowRecord(sil GrafanaSilence) (MaintenanceWindowRecord, bool) {
	if len(sil.Matchers) != 2 {
		return MaintenanceWindowRecord{}, false
	}
	var orgID int64
	var ruleUID string
	for _, m := range sil.Matchers {
		if !m.IsEqual || m.IsRegex {
			return MaintenanceWindowRecord{}, false
		}
		switch m.Name {
		case silenceLabelOrganizationID:
			orgID, _ = strconv.ParseInt(m.Value, 10, 64)
		// alertname_uid is the rule matcher name silences carried before alertRuleUIDMatcher.
		case alertRuleUIDMatcher, "alertname_uid":
			ruleUID = m.Value
		default:
			return MaintenanceWindowRecord{}, false
		}
	}
	if orgID <= 0 || ruleUID == "" {
		return MaintenanceWindowRecord{}, false
	}
	return MaintenanceWindowRecord{
		OrganizationID: orgID,
		RuleUIDs:       []string{ruleUID},
		// Postgres keeps microseconds; truncate so the already-migrated comparison round-trips.
		StartsAt:  sil.StartsAt.Truncate(time.Microsecond),
		EndsAt:    sil.EndsAt.Truncate(time.Microsecond),
		Comment:   strings.TrimSpace(strings.TrimPrefix(sil.Comment, legacyMaintenanceWindowCommentMarker)),
		CreatedBy: sil.CreatedBy,
	}, true
}

// hasEquivalentWindow reports whether ws already holds the legacy record (same single rule,
// every channel, same interval) — the trace of a previous sweep that inserted the row but
// failed before deleting the silence.
func hasEquivalentWindow(ws []MaintenanceWindowRecord, rec MaintenanceWindowRecord) bool {
	for _, w := range ws {
		if len(w.RuleUIDs) == 1 && w.RuleUIDs[0] == rec.RuleUIDs[0] &&
			len(w.ChannelIDs) == 0 &&
			w.StartsAt.Equal(rec.StartsAt) && w.EndsAt.Equal(rec.EndsAt) {
			return true
		}
	}
	return false
}
