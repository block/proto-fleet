package alerts

import (
	"bytes"
	"context"
	"encoding/base64"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
)

// testCipher is a real AES-GCM cipher with a fixed key, so encryption round-trips are exercised.
func testCipher(t *testing.T) Cipher {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	c, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: key})
	require.NoError(t, err)
	return c
}

type fakeChannelStore struct {
	rows     map[int64]ChannelRecord
	next     int64
	listErr  error
	inserted []ChannelRecord
}

func newFakeChannelStore() *fakeChannelStore {
	return &fakeChannelStore{rows: map[int64]ChannelRecord{}}
}

func (f *fakeChannelStore) Insert(_ context.Context, rec ChannelRecord) (ChannelRecord, error) {
	f.next++
	rec.ID = f.next
	f.rows[rec.ID] = rec
	f.inserted = append(f.inserted, rec)
	return rec, nil
}

func (f *fakeChannelStore) Update(_ context.Context, rec ChannelRecord) (ChannelRecord, error) {
	cur, ok := f.rows[rec.ID]
	if !ok || cur.OrganizationID != rec.OrganizationID {
		return ChannelRecord{}, ErrNotFound
	}
	f.rows[rec.ID] = rec
	return rec, nil
}

func (f *fakeChannelStore) Get(_ context.Context, orgID, id int64) (ChannelRecord, error) {
	rec, ok := f.rows[id]
	if !ok || rec.OrganizationID != orgID {
		return ChannelRecord{}, ErrNotFound
	}
	return rec, nil
}

func (f *fakeChannelStore) GetByName(_ context.Context, orgID int64, name string) (ChannelRecord, error) {
	for _, rec := range f.rows {
		if rec.OrganizationID == orgID && rec.Name == name {
			return rec, nil
		}
	}
	return ChannelRecord{}, ErrNotFound
}

func (f *fakeChannelStore) List(_ context.Context, orgID int64) ([]ChannelRecord, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []ChannelRecord
	for _, rec := range f.rows {
		if rec.OrganizationID == orgID {
			out = append(out, rec)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeChannelStore) SoftDelete(_ context.Context, orgID, id int64) error {
	rec, ok := f.rows[id]
	if !ok || rec.OrganizationID != orgID {
		return ErrNotFound
	}
	delete(f.rows, id)
	return nil
}

type stubTester struct {
	ok       bool
	errMsg   string
	gotKind  ChannelKind
	gotURL   string
	gotBrear string
}

func (s *stubTester) SendTest(_ context.Context, kind ChannelKind, url, bearer string) (bool, string, error) {
	s.gotKind, s.gotURL, s.gotBrear = kind, url, bearer
	return s.ok, s.errMsg, nil
}

// allowPrivate keeps the SSRF check from doing real DNS on test hostnames.
func newChannelService(t *testing.T) (*Service, *fakeChannelStore, *stubTester) {
	t.Helper()
	store := newFakeChannelStore()
	tester := &stubTester{ok: true}
	svc := NewService(nil, store, nil, testCipher(t), tester, DestinationPolicy{AllowPrivateDestinations: true})
	return svc, store, tester
}

const testSlackURL = "https://hooks.slack.com/services/T00/B00/SECRET"

func TestCreateChannelStoresEncryptedSecret(t *testing.T) {
	svc, store, _ := newChannelService(t)

	out, err := svc.CreateChannel(context.Background(), 7, Channel{
		Name:  "oncall",
		Kind:  ChannelKindSlack,
		Slack: &SlackConfig{WebhookURL: testSlackURL},
	})
	require.NoError(t, err)
	assert.True(t, out.HasSecret)
	require.NotNil(t, out.Slack)
	assert.Empty(t, out.Slack.WebhookURL, "read must not echo the secret url")

	require.Len(t, store.inserted, 1)
	rec := store.inserted[0]
	assert.Equal(t, ChannelKindSlack, rec.Kind)
	assert.NotContains(t, rec.EncryptedConfig, "hooks.slack.com", "config must be encrypted at rest")
	cfg, err := decodeChannelConfig(testCipher(t), rec.EncryptedConfig)
	require.NoError(t, err)
	assert.Equal(t, testSlackURL, cfg.URL)
}

func TestCreateChannelRejectsDuplicateName(t *testing.T) {
	svc, _, _ := newChannelService(t)
	_, err := svc.CreateChannel(context.Background(), 7, Channel{Name: "dup", Kind: ChannelKindSlack, Slack: &SlackConfig{WebhookURL: testSlackURL}})
	require.NoError(t, err)

	_, err = svc.CreateChannel(context.Background(), 7, Channel{Name: "dup", Kind: ChannelKindSlack, Slack: &SlackConfig{WebhookURL: testSlackURL}})
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err))
}

func TestCreateChannelAllowsSameNameDifferentOrg(t *testing.T) {
	svc, _, _ := newChannelService(t)
	_, err := svc.CreateChannel(context.Background(), 7, Channel{Name: "ops", Kind: ChannelKindSlack, Slack: &SlackConfig{WebhookURL: testSlackURL}})
	require.NoError(t, err)
	_, err = svc.CreateChannel(context.Background(), 8, Channel{Name: "ops", Kind: ChannelKindSlack, Slack: &SlackConfig{WebhookURL: testSlackURL}})
	require.NoError(t, err)
}

func TestListChannelsRedactsSecrets(t *testing.T) {
	svc, _, _ := newChannelService(t)
	_, err := svc.CreateChannel(context.Background(), 7, Channel{
		Name: "hook", Kind: ChannelKindWebhook,
		Webhook: &WebhookConfig{URL: "https://relay.example.com/path?token=abc", BearerHeader: "s3cret"},
	})
	require.NoError(t, err)

	channels, err := svc.ListChannels(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, channels, 1)
	c := channels[0]
	assert.True(t, c.HasSecret)
	require.NotNil(t, c.Webhook)
	assert.Equal(t, "https://relay.example.com", c.Webhook.URL, "webhook url must be host-only")
	assert.NotContains(t, c.Webhook.URL, "token=")
}

func TestUpdateChannelPreservesSlackSecretOnRename(t *testing.T) {
	svc, store, _ := newChannelService(t)
	created, err := svc.CreateChannel(context.Background(), 7, Channel{Name: "old", Kind: ChannelKindSlack, Slack: &SlackConfig{WebhookURL: testSlackURL}})
	require.NoError(t, err)

	updated, err := svc.UpdateChannel(context.Background(), 7, Channel{ID: created.ID, Name: "new", Kind: ChannelKindSlack, Slack: &SlackConfig{}})
	require.NoError(t, err)
	assert.True(t, updated.HasSecret)
	assert.Equal(t, "new", updated.Name)

	id, _ := parseChannelID(created.ID)
	cfg, err := decodeChannelConfig(testCipher(t), store.rows[id].EncryptedConfig)
	require.NoError(t, err)
	assert.Equal(t, testSlackURL, cfg.URL, "rename with no new url keeps the stored secret")
}

func TestUpdateChannelReplacesSlackURL(t *testing.T) {
	svc, store, _ := newChannelService(t)
	created, err := svc.CreateChannel(context.Background(), 7, Channel{Name: "s", Kind: ChannelKindSlack, Slack: &SlackConfig{WebhookURL: testSlackURL}})
	require.NoError(t, err)

	fresh := "https://hooks.slack.com/services/T99/B99/NEW"
	_, err = svc.UpdateChannel(context.Background(), 7, Channel{ID: created.ID, Name: "s", Kind: ChannelKindSlack, Slack: &SlackConfig{WebhookURL: fresh}})
	require.NoError(t, err)

	id, _ := parseChannelID(created.ID)
	cfg, err := decodeChannelConfig(testCipher(t), store.rows[id].EncryptedConfig)
	require.NoError(t, err)
	assert.Equal(t, fresh, cfg.URL)
}

func TestUpdateChannelPreservesWebhookBearerWhenDestinationUnchanged(t *testing.T) {
	svc, store, _ := newChannelService(t)
	created, err := svc.CreateChannel(context.Background(), 7, Channel{
		Name: "w", Kind: ChannelKindWebhook,
		Webhook: &WebhookConfig{URL: "https://relay.example.com/hook", BearerHeader: "tok"},
	})
	require.NoError(t, err)

	// Resubmit the redacted host-only URL with no new bearer (what a rename-only edit sends).
	updated, err := svc.UpdateChannel(context.Background(), 7, Channel{
		ID: created.ID, Name: "w2", Kind: ChannelKindWebhook,
		Webhook: &WebhookConfig{URL: "https://relay.example.com"},
	})
	require.NoError(t, err)
	assert.True(t, updated.HasSecret)

	id, _ := parseChannelID(created.ID)
	cfg, err := decodeChannelConfig(testCipher(t), store.rows[id].EncryptedConfig)
	require.NoError(t, err)
	assert.Equal(t, "https://relay.example.com/hook", cfg.URL, "unchanged destination keeps the full stored url")
	assert.Equal(t, "tok", cfg.Bearer, "unchanged destination carries the stored bearer")
}

func TestUpdateChannelClearsWebhookBearerWhenRequested(t *testing.T) {
	svc, store, _ := newChannelService(t)
	created, err := svc.CreateChannel(context.Background(), 7, Channel{
		Name: "w", Kind: ChannelKindWebhook,
		Webhook: &WebhookConfig{URL: "https://relay.example.com/hook", BearerHeader: "tok"},
	})
	require.NoError(t, err)

	// Keep the destination but explicitly revoke the bearer (no URL change required).
	updated, err := svc.UpdateChannel(context.Background(), 7, Channel{
		ID: created.ID, Name: "w", Kind: ChannelKindWebhook,
		Webhook: &WebhookConfig{URL: "https://relay.example.com", ClearBearer: true},
	})
	require.NoError(t, err)
	assert.False(t, updated.HasSecret, "clearing the bearer drops the stored secret")

	id, _ := parseChannelID(created.ID)
	cfg, err := decodeChannelConfig(testCipher(t), store.rows[id].EncryptedConfig)
	require.NoError(t, err)
	assert.Equal(t, "https://relay.example.com/hook", cfg.URL, "destination is preserved")
	assert.Empty(t, cfg.Bearer, "bearer is revoked")
}

func TestUpdateChannelDropsBearerOnDestinationChange(t *testing.T) {
	svc, store, _ := newChannelService(t)
	created, err := svc.CreateChannel(context.Background(), 7, Channel{
		Name: "w", Kind: ChannelKindWebhook,
		Webhook: &WebhookConfig{URL: "https://relay.example.com/hook", BearerHeader: "tok"},
	})
	require.NoError(t, err)

	updated, err := svc.UpdateChannel(context.Background(), 7, Channel{
		ID: created.ID, Name: "w", Kind: ChannelKindWebhook,
		Webhook: &WebhookConfig{URL: "https://other.example.com/hook"},
	})
	require.NoError(t, err)
	assert.False(t, updated.HasSecret, "a new destination must not inherit the old bearer")

	id, _ := parseChannelID(created.ID)
	cfg, err := decodeChannelConfig(testCipher(t), store.rows[id].EncryptedConfig)
	require.NoError(t, err)
	assert.Equal(t, "https://other.example.com/hook", cfg.URL)
	assert.Empty(t, cfg.Bearer)
}

func TestDeleteChannel(t *testing.T) {
	svc, _, _ := newChannelService(t)
	created, err := svc.CreateChannel(context.Background(), 7, Channel{Name: "d", Kind: ChannelKindSlack, Slack: &SlackConfig{WebhookURL: testSlackURL}})
	require.NoError(t, err)

	require.NoError(t, svc.DeleteChannel(context.Background(), 7, created.ID))
	require.ErrorIs(t, svc.DeleteChannel(context.Background(), 7, created.ID), ErrNotFound)
}

func TestUpdateChannelRejectsForeignOrg(t *testing.T) {
	svc, _, _ := newChannelService(t)
	created, err := svc.CreateChannel(context.Background(), 7, Channel{Name: "x", Kind: ChannelKindSlack, Slack: &SlackConfig{WebhookURL: testSlackURL}})
	require.NoError(t, err)

	_, err = svc.UpdateChannel(context.Background(), 9, Channel{ID: created.ID, Name: "x", Kind: ChannelKindSlack, Slack: &SlackConfig{}})
	require.ErrorIs(t, err, ErrNotFound)
}

func TestTestChannelSavedUsesStoredSecret(t *testing.T) {
	svc, _, tester := newChannelService(t)
	created, err := svc.CreateChannel(context.Background(), 7, Channel{Name: "s", Kind: ChannelKindSlack, Slack: &SlackConfig{WebhookURL: testSlackURL}})
	require.NoError(t, err)

	ok, code, _, err := svc.TestChannel(context.Background(), 7, Channel{ID: created.ID})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 200, code)
	assert.Equal(t, ChannelKindSlack, tester.gotKind)
	assert.Equal(t, testSlackURL, tester.gotURL, "saved-channel test must send the decrypted stored url")
}

func TestTestChannelBeforeSaveSendsSubmittedDestination(t *testing.T) {
	svc, _, tester := newChannelService(t)
	ok, _, _, err := svc.TestChannel(context.Background(), 7, Channel{Kind: ChannelKindSlack, Slack: &SlackConfig{WebhookURL: testSlackURL}})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, testSlackURL, tester.gotURL)
}

func TestTestChannelRejectsForeignSavedChannel(t *testing.T) {
	svc, _, _ := newChannelService(t)
	created, err := svc.CreateChannel(context.Background(), 7, Channel{Name: "s", Kind: ChannelKindSlack, Slack: &SlackConfig{WebhookURL: testSlackURL}})
	require.NoError(t, err)

	_, _, _, err = svc.TestChannel(context.Background(), 9, Channel{ID: created.ID})
	require.ErrorIs(t, err, ErrNotFound)
}
