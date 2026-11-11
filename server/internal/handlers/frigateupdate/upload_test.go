package frigateupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/authn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/frigaterelease"
	domain "github.com/block/proto-fleet/server/internal/domain/frigateupdate"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/testutil"
)

type allowAll struct{ denied bool }

func (a *allowAll) AuthorizeSite(context.Context, *int64) error { return a.err() }
func (a *allowAll) AuthorizeAnySite(context.Context) error      { return a.err() }
func (a *allowAll) AuthorizeOrgWide(context.Context) error      { return a.err() }
func (a *allowAll) err() error {
	if a.denied {
		return fleeterror.NewForbiddenError("denied")
	}
	return nil
}

// headerAuthenticator maps the X-Test-User header to a session user.
type headerAuthenticator struct {
	users map[string]*session.Info
}

func (h headerAuthenticator) AuthenticateRequest(ctx context.Context, headers http.Header) (context.Context, error) {
	info, ok := h.users[headers.Get("X-Test-User")]
	if !ok {
		return ctx, fleeterror.NewUnauthenticatedError("authentication required")
	}
	return authn.SetInfo(ctx, info), nil
}

func controlTarGz(t *testing.T, control string) []byte {
	t.Helper()
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "./control", Mode: 0o644, Size: int64(len(control)), Typeflag: tar.TypeReg}))
	_, err := tw.Write([]byte(control))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, err = zw.Write(tarBuf.Bytes())
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return gz.Bytes()
}

// buildDeb returns a deb of at least minSize bytes so uploads need several chunks.
func buildDeb(t *testing.T, minSize int) []byte {
	t.Helper()
	var out bytes.Buffer
	out.WriteString("!<arch>\n")
	member := func(name string, data []byte) {
		fmt.Fprintf(&out, "%-16s%-12d%-6d%-6d%-8s%-10d`\n", name+"/", 0, 0, 0, "100644", len(data))
		out.Write(data)
		if len(data)%2 == 1 {
			out.WriteByte('\n')
		}
	}
	member("debian-binary", []byte("2.0\n"))
	member("control.tar.gz", controlTarGz(t, "Package: frigate\nVersion: 2.1.0\nArchitecture: arm64\nMaintainer: x <x@example.com>\nDescription: t\n"))
	payload := make([]byte, minSize)
	_, err := rand.Read(payload)
	require.NoError(t, err)
	member("data.tar.gz", payload)
	return out.Bytes()
}

type uploadEnv struct {
	t        *testing.T
	handlers Handlers
	deb      []byte
	statemnt []byte
	sig      []byte
	priv     *ecdsa.PrivateKey
	svc      *domain.Service
	auth     *allowAll
	mgr      *UploadManager
}

func newUploadEnv(t *testing.T) *uploadEnv {
	t.Helper()
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	alice := database.CreateSuperAdminUser()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	key, err := frigaterelease.NewTrustedKey(&priv.PublicKey)
	require.NoError(t, err)
	store, err := files.NewFrigatePackageStore(t.TempDir())
	require.NoError(t, err)
	auth := &allowAll{}
	svc := domain.NewService(database.DB, nil, store, frigaterelease.NewStaticVerifier(key), auth, nil, domain.Config{})
	mgr := NewUploadManager()

	authenticator := headerAuthenticator{users: map[string]*session.Info{
		"alice": {UserID: alice.DatabaseID, OrganizationID: alice.OrganizationID, AuthMethod: session.AuthMethodSession, Username: alice.Username, ExternalUserID: "ext-alice"},
		"bob":   {UserID: alice.DatabaseID + 1000, OrganizationID: alice.OrganizationID, AuthMethod: session.AuthMethodSession, Username: "bob", ExternalUserID: "ext-bob"},
	}}
	e := &uploadEnv{t: t, svc: svc, auth: auth, mgr: mgr, priv: priv,
		handlers: NewUploadHandlers(mgr, svc, store, auth, authenticator)}
	e.deb = buildDeb(t, 2<<20+123)
	sum := sha256.Sum256(e.deb)
	e.statemnt = e.statement(func(s *frigaterelease.Statement) { s.SHA256 = hex.EncodeToString(sum[:]); s.Size = int64(len(e.deb)) })
	e.sig = e.sign(e.statemnt)
	return e
}

// statement returns the canonical signed encoding of a statement for e.deb.
func (e *uploadEnv) statement(mutate func(*frigaterelease.Statement)) []byte {
	s := frigaterelease.Statement{
		Schema: frigaterelease.SchemaV1, Package: "frigate", Version: "2.1.0", Architecture: "arm64",
		Repository: "btc-mining/frigate", MinInstalledVersion: "1.0.0", IssuedAt: time.Now().UTC(),
	}
	if mutate != nil {
		mutate(&s)
	}
	raw, err := frigaterelease.MarshalCanonical(s)
	require.NoError(e.t, err)
	return raw
}

func (e *uploadEnv) sign(statement []byte) []byte {
	digest := sha256.Sum256(statement)
	sig, err := ecdsa.SignASN1(rand.Reader, e.priv, digest[:])
	require.NoError(e.t, err)
	return sig
}

func (e *uploadEnv) initiate(user string, body initiateRequest) *httptest.ResponseRecorder {
	raw, err := json.Marshal(body)
	require.NoError(e.t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/frigate-packages/upload/chunked", bytes.NewReader(raw))
	req.Header.Set("X-Test-User", user)
	rec := httptest.NewRecorder()
	e.handlers.Initiate.ServeHTTP(rec, req)
	return rec
}

func (e *uploadEnv) validBody() initiateRequest {
	return initiateRequest{
		Filename: "frigate_2.1.0_arm64.deb", Size: int64(len(e.deb)),
		ReleaseStatementB64: base64.StdEncoding.EncodeToString(e.statemnt), ReleaseSignatureB64: base64.StdEncoding.EncodeToString(e.sig),
	}
}

func (e *uploadEnv) put(user, id string, start, end int, total int, data []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/api/v1/frigate-packages/upload/chunked/"+id, bytes.NewReader(data))
	req.SetPathValue("uploadId", id)
	req.Header.Set("X-Test-User", user)
	req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
	rec := httptest.NewRecorder()
	e.handlers.Chunk.ServeHTTP(rec, req)
	return rec
}

func (e *uploadEnv) complete(user, id string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/frigate-packages/upload/chunked/"+id+"/complete", nil)
	req.SetPathValue("uploadId", id)
	req.Header.Set("X-Test-User", user)
	rec := httptest.NewRecorder()
	e.handlers.Complete.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v), rec.Body.String())
	return v
}

func (e *uploadEnv) upload(user string) completeResponse {
	rec := e.initiate(user, e.validBody())
	require.Equal(e.t, http.StatusOK, rec.Code, rec.Body.String())
	init := decode[initiateResponse](e.t, rec)
	assert.Equal(e.t, "2.1.0", init.Version)
	assert.Equal(e.t, "arm64", init.Architecture)
	const chunk = 1 << 20
	for start := 0; start < len(e.deb); start += chunk {
		end := min(start+chunk, len(e.deb))
		rec := e.put(user, init.UploadID, start, end-1, len(e.deb), e.deb[start:end])
		require.Equal(e.t, http.StatusOK, rec.Code, rec.Body.String())
	}
	rec = e.complete(user, init.UploadID)
	require.Equal(e.t, http.StatusOK, rec.Code, rec.Body.String())
	return decode[completeResponse](e.t, rec)
}

func TestUpload_FullFlowAndReuse(t *testing.T) {
	e := newUploadEnv(t)
	first := e.upload("alice")
	assert.False(t, first.Reused)
	assert.Equal(t, "2.1.0", first.Package.Version)
	assert.Equal(t, int64(len(e.deb)), first.Package.SizeBytes)
	assert.NotEmpty(t, first.Package.ReleaseKeyID)

	second := e.upload("alice")
	assert.True(t, second.Reused)
	assert.Equal(t, first.Package.ID, second.Package.ID)
}

func TestUpload_Initiate_Rejections(t *testing.T) {
	e := newUploadEnv(t)

	assert.Equal(t, http.StatusUnauthorized, e.initiate("nobody", e.validBody()).Code)

	e.auth.denied = true
	assert.Equal(t, http.StatusForbidden, e.initiate("alice", e.validBody()).Code)
	e.auth.denied = false

	body := e.validBody()
	body.ReleaseSignatureB64 = base64.StdEncoding.EncodeToString(e.sign([]byte("different statement")))
	assert.Equal(t, http.StatusBadRequest, e.initiate("alice", body).Code, "bad signature fails before any bytes are accepted")

	body = e.validBody()
	body.Size++
	assert.Equal(t, http.StatusBadRequest, e.initiate("alice", body).Code)

	body = e.validBody()
	body.ReleaseStatementB64 = "!!!not-base64"
	assert.Equal(t, http.StatusBadRequest, e.initiate("alice", body).Code)

	body = e.validBody()
	body.Filename = "../x.deb"
	assert.Equal(t, http.StatusBadRequest, e.initiate("alice", body).Code)

	req := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader([]byte("{not json")))
	req.Header.Set("X-Test-User", "alice")
	rec := httptest.NewRecorder()
	e.handlers.Initiate.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	req = httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(make([]byte, maxInitiateBody+1)))
	req.Header.Set("X-Test-User", "alice")
	rec = httptest.NewRecorder()
	e.handlers.Initiate.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUpload_Chunk_Rejections(t *testing.T) {
	e := newUploadEnv(t)
	init := decode[initiateResponse](t, e.initiate("alice", e.validBody()))
	total := len(e.deb)

	// Another user cannot touch the session.
	assert.Equal(t, http.StatusNotFound, e.put("bob", init.UploadID, 0, 9, total, e.deb[:10]).Code)
	assert.Equal(t, http.StatusNotFound, e.complete("bob", init.UploadID).Code)
	assert.Equal(t, http.StatusNotFound, e.put("alice", "unknown", 0, 9, total, e.deb[:10]).Code)

	// Wrong total, wrong offset, short body, garbage header.
	assert.Equal(t, http.StatusBadRequest, e.put("alice", init.UploadID, 0, 9, total+1, e.deb[:10]).Code)
	assert.Equal(t, http.StatusBadRequest, e.put("alice", init.UploadID, 5, 14, total, e.deb[5:15]).Code)
	assert.Equal(t, http.StatusBadRequest, e.put("alice", init.UploadID, 0, 9, total, e.deb[:5]).Code)
	req := httptest.NewRequest(http.MethodPut, "/x", bytes.NewReader(e.deb[:10]))
	req.SetPathValue("uploadId", init.UploadID)
	req.Header.Set("X-Test-User", "alice")
	rec := httptest.NewRecorder()
	e.handlers.Chunk.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "missing Content-Range")

	// A session cannot complete early.
	assert.Equal(t, http.StatusBadRequest, e.complete("alice", init.UploadID).Code)
	assert.Equal(t, http.StatusNotFound, e.complete("alice", init.UploadID).Code, "the failed session is gone")
}

func TestUpload_CorruptedBytesAreRejectedAtComplete(t *testing.T) {
	e := newUploadEnv(t)
	init := decode[initiateResponse](t, e.initiate("alice", e.validBody()))
	bad := append([]byte(nil), e.deb...)
	bad[len(bad)-1] ^= 0xff
	require.Equal(t, http.StatusOK, e.put("alice", init.UploadID, 0, len(bad)-1, len(bad), bad).Code)
	rec := e.complete("alice", init.UploadID)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "SHA-256")
}

func TestUpload_SessionCaps(t *testing.T) {
	e := newUploadEnv(t)
	// One user is held to the per-user cap, and another user is unaffected.
	for range maxUploadSessionsPerUser {
		require.Equal(t, http.StatusOK, e.initiate("alice", e.validBody()).Code)
	}
	assert.Equal(t, http.StatusTooManyRequests, e.initiate("alice", e.validBody()).Code)
	assert.Equal(t, http.StatusOK, e.initiate("bob", e.validBody()).Code)

	// Fill the global cap with sessions of other users: nobody gets another.
	e.mgr.mu.Lock()
	for i := len(e.mgr.sessions); i < maxUploadSessions; i++ {
		e.mgr.sessions[fmt.Sprintf("other-%d", i)] = &uploadSession{owner: uploadOwner{organizationID: 1, userID: int64(100 + i)}, lastActivity: time.Now()}
	}
	e.mgr.mu.Unlock()
	assert.Equal(t, http.StatusTooManyRequests, e.initiate("bob", e.validBody()).Code)
}

func TestUpload_IdleSessionsAreReclaimedAtTheCap(t *testing.T) {
	e := newUploadEnv(t)
	for range maxUploadSessionsPerUser {
		require.Equal(t, http.StatusOK, e.initiate("alice", e.validBody()).Code)
	}
	// Both uploads were abandoned a while ago, so they no longer block a new one.
	e.mgr.mu.Lock()
	stale := make([]string, 0, len(e.mgr.sessions))
	for id, sess := range e.mgr.sessions {
		sess.lastActivity = time.Now().Add(-2 * idleSessionReclaim)
		stale = append(stale, id)
	}
	e.mgr.mu.Unlock()
	assert.Equal(t, http.StatusOK, e.initiate("alice", e.validBody()).Code)
	e.mgr.mu.Lock()
	for _, id := range stale {
		assert.NotContains(t, e.mgr.sessions, id)
	}
	e.mgr.mu.Unlock()

	// A recently active upload is never reclaimed.
	require.Equal(t, http.StatusOK, e.initiate("alice", e.validBody()).Code)
	assert.Equal(t, http.StatusTooManyRequests, e.initiate("alice", e.validBody()).Code)

	// Other users' abandoned uploads free the global cap too.
	e.mgr.mu.Lock()
	for i := len(e.mgr.sessions); i < maxUploadSessions; i++ {
		e.mgr.sessions[fmt.Sprintf("other-%d", i)] = &uploadSession{owner: uploadOwner{organizationID: 1, userID: int64(100 + i)}, lastActivity: time.Now().Add(-2 * idleSessionReclaim), tempFilePath: t.TempDir() + "/missing"}
	}
	e.mgr.mu.Unlock()
	assert.Equal(t, http.StatusOK, e.initiate("bob", e.validBody()).Code)
}

func TestWriteServiceError_StatusMapping(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{fleeterror.NewUnauthenticatedError("x"), http.StatusUnauthorized},
		{fleeterror.NewForbiddenError("x"), http.StatusForbidden},
		{fleeterror.NewInvalidArgumentError("x"), http.StatusBadRequest},
		{fleeterror.NewNotFoundError("x"), http.StatusNotFound},
		{fleeterror.NewFailedPreconditionError("x"), http.StatusConflict},
		{fleeterror.NewUnavailableErrorf("controller unavailable"), http.StatusServiceUnavailable},
		{fleeterror.NewInternalError("boom"), http.StatusInternalServerError},
	} {
		rec := httptest.NewRecorder()
		writeServiceError(rec, "test", tc.err)
		assert.Equal(t, tc.status, rec.Code, tc.err)
	}
}

type fakeSweeper struct {
	calls int
	err   error
}

func (f *fakeSweeper) SweepOrphanPackages(context.Context) (int, error) {
	f.calls++
	return 0, f.err
}

func TestUploadManager_SweepRunsOrphanSweepAndSurvivesItsFailure(t *testing.T) {
	m := NewUploadManager()
	m.sessions["old"] = &uploadSession{lastActivity: time.Now().Add(-2 * time.Hour), tempFilePath: t.TempDir() + "/missing"}
	sweeper := &fakeSweeper{err: assert.AnError}
	m.sweep(t.Context(), time.Hour, sweeper)
	assert.Equal(t, 1, sweeper.calls)
	assert.NotContains(t, m.sessions, "old", "a failing orphan sweep does not stop session cleanup")
	m.sweep(t.Context(), time.Hour, nil) // A nil sweeper only expires sessions.
}

func TestUploadManager_CleanupExpired(t *testing.T) {
	m := NewUploadManager()
	m.sessions["old"] = &uploadSession{lastActivity: time.Now().Add(-2 * time.Hour), tempFilePath: t.TempDir() + "/missing"}
	m.sessions["new"] = &uploadSession{lastActivity: time.Now(), tempFilePath: t.TempDir() + "/missing"}
	m.cleanupExpired(time.Hour)
	assert.NotContains(t, m.sessions, "old")
	assert.Contains(t, m.sessions, "new")
}

func TestParseContentRange(t *testing.T) {
	s, e, total, err := parseContentRange("bytes 0-9/100")
	require.NoError(t, err)
	assert.Equal(t, []int64{0, 9, 100}, []int64{s, e, total})
	for _, bad := range []string{"", "0-9/100", "bytes 0-9", "bytes 0/100", "bytes a-9/100", "bytes 9-0/100", "bytes 0-100/100", "bytes 0-9/0"} {
		_, _, _, err := parseContentRange(bad)
		assert.Error(t, err, bad)
	}
}
