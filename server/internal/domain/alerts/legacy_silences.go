package alerts

// Pre-migration compat: maintenance windows used to be stored as Grafana silences. Everything
// needed to recognize and remove those silences lives here so it can be deleted as one unit once
// every deployment has run the startup sweep (the DeleteRule cleanup in user_rules.go also calls
// isLegacyMaintenanceWindowSilence).

import (
	"context"
	"strings"
)

// The comment marker maintenance windows carried while they were stored as Grafana silences
// (before they moved to the database and delivery-side muting). Still recognized so the startup
// sweep and rule-deletion cleanup can find pre-migration silences.
const legacyMaintenanceWindowCommentMarker = "[proto-fleet-mw]"

func isLegacyMaintenanceWindowSilence(sil GrafanaSilence) bool {
	return strings.HasPrefix(sil.Comment, legacyMaintenanceWindowCommentMarker)
}

// CleanupLegacyMaintenanceWindowSilences deletes the Grafana silences that backed maintenance
// windows before they moved to the database. Without this, pre-migration windows would keep
// muting alerts at the Alertmanager — invisibly, since the list no longer reads Grafana. Runs
// across all orgs at startup; operators recreate any window they still need.
func (s *Service) CleanupLegacyMaintenanceWindowSilences(ctx context.Context) (int, error) {
	sils, err := s.grafana.ListSilences(ctx)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, sil := range sils {
		if !isLegacyMaintenanceWindowSilence(sil) {
			continue
		}
		// Expired silences are inert and Grafana garbage-collects them; deleting would just churn.
		if sil.Status != nil && sil.Status.State == "expired" {
			continue
		}
		if err := s.grafana.DeleteSilence(ctx, sil.ID); err != nil && !IsNotFound(err) {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
