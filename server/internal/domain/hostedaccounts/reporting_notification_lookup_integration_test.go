package hostedaccounts_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/alerts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/stretchr/testify/require"
)

type failingReportingChannelLookup struct {
	alerts.ChannelStore
	fail bool
}

func (s *failingReportingChannelLookup) List(ctx context.Context, org int64) ([]alerts.ChannelRecord, error) {
	if s.fail {
		return nil, errors.New("temporary destination lookup error")
	}
	return s.ChannelStore.List(ctx, org)
}

func TestReportingNotificationLookupFailureRetriesAfterRecovery(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprint(partial), func(t *testing.T) {
			f, client, _, _ := notificationReviewFixture(t)
			store := &failingReportingChannelLookup{ChannelStore: sqlstores.NewSQLAlertChannelStore(f.db)}
			crypto, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="})
			require.NoError(t, err)
			var healthyPosts, pendingPosts atomic.Int32
			var available atomic.Bool
			healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { healthyPosts.Add(1); w.WriteHeader(http.StatusOK) }))
			t.Cleanup(healthy.Close)
			pending := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				pendingPosts.Add(1)
				if !available.Load() {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(pending.Close)
			for name, destination := range map[string]string{"healthy": healthy.URL, "recovering": pending.URL} {
				body, err := json.Marshal(map[string]string{"url": destination})
				require.NoError(t, err)
				encrypted, err := crypto.Encrypt(body)
				require.NoError(t, err)
				_, err = store.Insert(t.Context(), alerts.ChannelRecord{OrganizationID: f.org, Name: name, Kind: alerts.ChannelKindWebhook, EncryptedConfig: encrypted, ValidationState: alerts.ValidationOK})
				require.NoError(t, err)
			}
			notifier := alerts.NewDeliverer(store, nil, nil, crypto, nil, alerts.DestinationPolicy{AllowPrivateDestinations: true}, "")
			jobs := hostedaccounts.NewReportingJobs(f.svc, notifier)
			now := time.Now().UTC().Round(time.Microsecond)
			if partial {
				require.NoError(t, jobs.Tick(t.Context(), now))
			}
			store.fail = true
			require.NoError(t, jobs.Tick(t.Context(), now.Add(2*time.Minute)))
			var status string
			var delivered int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT notification_status,cardinality(delivered_channel_ids) FROM hosted_report_queue WHERE account_id=$1`, client.ID).Scan(&status, &delivered))
			require.Equal(t, "failed", status, "a transient lookup must not become sent or unrouted")
			if partial {
				require.Equal(t, 1, delivered)
			} else {
				require.Zero(t, delivered)
			}
			store.fail = false
			available.Store(true)
			require.NoError(t, jobs.Tick(t.Context(), now.Add(10*time.Minute)))
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT notification_status,cardinality(delivered_channel_ids) FROM hosted_report_queue WHERE account_id=$1`, client.ID).Scan(&status, &delivered))
			require.Equal(t, "sent", status)
			require.Equal(t, 2, delivered)
			require.EqualValues(t, 1, healthyPosts.Load(), "an accepted destination never receives a duplicate")
			if partial {
				require.EqualValues(t, 2, pendingPosts.Load())
			} else {
				require.EqualValues(t, 1, pendingPosts.Load())
			}
		})
	}
}
