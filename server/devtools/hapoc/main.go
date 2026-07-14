package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	defaultHTTPAddr          = "0.0.0.0:4080"
	defaultLeaseName         = "fleet-active"
	defaultLeaseTTL          = 6 * time.Second
	defaultRenewInterval     = 2 * time.Second
	defaultAcquireInterval   = 1 * time.Second
	defaultHeartbeatInterval = 1 * time.Second
	defaultOperationTimeout  = 2 * time.Second
	defaultConnMaxLifetime   = 3 * time.Second
)

type config struct {
	HTTPAddr              string
	DBDSN                 string
	HostID                string
	LeaseName             string
	LeaseTTL              time.Duration
	RenewInterval         time.Duration
	AcquireInterval       time.Duration
	HeartbeatInterval     time.Duration
	OperationTimeout      time.Duration
	ConnMaxLifetime       time.Duration
	StatusToken           string
	RequireMultiHostDSN   bool
	RequireReadWriteAttrs bool
}

type app struct {
	cfg      config
	holderID string

	dbMu sync.RWMutex
	db   *sql.DB

	mu              sync.RWMutex
	active          bool
	activeSince     time.Time
	leaseEpoch      int64
	leaseExpiresAt  time.Time
	lastRenewAt     time.Time
	lastHeartbeatAt time.Time
	lastDBReadyAt   time.Time
	lastDBPrimary   string
	lastError       string
	schemaReady     bool
	startedAt       time.Time
}

type statusSnapshot struct {
	HostID          string    `json:"host_id"`
	HolderID        string    `json:"holder_id"`
	Active          bool      `json:"active"`
	ActiveHealthy   bool      `json:"active_healthy"`
	ActiveSince     time.Time `json:"active_since,omitempty"`
	LeaseName       string    `json:"lease_name"`
	LeaseEpoch      int64     `json:"lease_epoch"`
	LeaseExpiresAt  time.Time `json:"lease_expires_at,omitempty"`
	LastRenewAt     time.Time `json:"last_renew_at,omitempty"`
	LastHeartbeatAt time.Time `json:"last_heartbeat_at,omitempty"`
	LastDBReadyAt   time.Time `json:"last_db_ready_at,omitempty"`
	LastDBPrimary   string    `json:"last_db_primary,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
	StartedAt       time.Time `json:"started_at"`
}

type leaseRow struct {
	Name       string    `json:"name"`
	HolderID   string    `json:"holder_id"`
	HostID     string    `json:"host_id"`
	Epoch      int64     `json:"lease_epoch"`
	ExpiresAt  time.Time `json:"expires_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	DBNow      time.Time `json:"db_now"`
	DBWritable bool      `json:"db_writable"`
}

type readiness struct {
	Ready      bool   `json:"ready"`
	DBWritable bool   `json:"db_writable"`
	DBAddress  string `json:"db_address,omitempty"`
}

func main() {
	if err := run(); err != nil {
		slog.Error("HA POC fake Fleet stopped", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	holderID, err := newHolderID(cfg.HostID)
	if err != nil {
		return fmt.Errorf("create holder identity: %w", err)
	}
	db, err := openDB(cfg)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	a := &app{
		cfg:       cfg,
		holderID:  holderID,
		db:        db,
		startedAt: time.Now().UTC(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	a.register(mux)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go a.runLeaseLoop(ctx)
	go a.runHeartbeatLoop(ctx)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Warn("http shutdown failed", slog.Any("error", err))
		}
	}()

	slog.Info("HA POC fake Fleet starting",
		slog.String("addr", cfg.HTTPAddr),
		slog.String("host_id", cfg.HostID),
		slog.String("holder_id", holderID),
		slog.String("db_dsn", redactDSN(cfg.DBDSN)))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen and serve: %w", err)
	}
	return nil
}

func loadConfig() (config, error) {
	hostID := strings.TrimSpace(os.Getenv("HA_POC_HOST_ID"))
	if hostID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return config{}, fmt.Errorf("read hostname: %w", err)
		}
		hostID = hostname
	}

	dbDSN := firstEnv("HA_POC_DB_DSN", "DB_DSN", "DATABASE_URL")
	cfg := config{
		HTTPAddr:              envOrDefault("HA_POC_HTTP_ADDR", defaultHTTPAddr),
		DBDSN:                 dbDSN,
		HostID:                hostID,
		LeaseName:             envOrDefault("HA_POC_LEASE_NAME", defaultLeaseName),
		LeaseTTL:              durationEnv("HA_POC_LEASE_TTL", defaultLeaseTTL),
		RenewInterval:         durationEnv("HA_POC_RENEW_INTERVAL", defaultRenewInterval),
		AcquireInterval:       durationEnv("HA_POC_ACQUIRE_INTERVAL", defaultAcquireInterval),
		HeartbeatInterval:     durationEnv("HA_POC_HEARTBEAT_INTERVAL", defaultHeartbeatInterval),
		OperationTimeout:      durationEnv("HA_POC_OPERATION_TIMEOUT", defaultOperationTimeout),
		ConnMaxLifetime:       durationEnv("HA_POC_CONN_MAX_LIFETIME", defaultConnMaxLifetime),
		StatusToken:           strings.TrimSpace(os.Getenv("HA_POC_STATUS_TOKEN")),
		RequireMultiHostDSN:   boolEnv("HA_POC_REQUIRE_MULTI_HOST_DSN"),
		RequireReadWriteAttrs: boolEnvDefault("HA_POC_REQUIRE_READ_WRITE_ATTRS", true),
	}
	if cfg.DBDSN == "" {
		return config{}, errors.New("HA_POC_DB_DSN, DB_DSN, or DATABASE_URL is required")
	}
	if cfg.LeaseTTL <= 0 {
		return config{}, errors.New("HA_POC_LEASE_TTL must be positive")
	}
	if cfg.RenewInterval <= 0 || cfg.RenewInterval >= cfg.LeaseTTL {
		return config{}, errors.New("HA_POC_RENEW_INTERVAL must be positive and less than HA_POC_LEASE_TTL")
	}
	if cfg.AcquireInterval <= 0 {
		return config{}, errors.New("HA_POC_ACQUIRE_INTERVAL must be positive")
	}
	if cfg.HeartbeatInterval <= 0 {
		return config{}, errors.New("HA_POC_HEARTBEAT_INTERVAL must be positive")
	}
	if cfg.OperationTimeout <= 0 {
		return config{}, errors.New("HA_POC_OPERATION_TIMEOUT must be positive")
	}
	if cfg.ConnMaxLifetime <= 0 {
		return config{}, errors.New("HA_POC_CONN_MAX_LIFETIME must be positive")
	}
	if cfg.RequireMultiHostDSN && !dsnLooksMultiHost(cfg.DBDSN) {
		return config{}, errors.New("HA_POC_DB_DSN must contain multiple DB hosts when HA_POC_REQUIRE_MULTI_HOST_DSN=true")
	}
	if cfg.RequireReadWriteAttrs && !dsnHasReadWriteTarget(cfg.DBDSN) {
		return config{}, errors.New("HA_POC_DB_DSN must include target_session_attrs=read-write")
	}
	return cfg, nil
}

func openDB(cfg config) (*sql.DB, error) {
	db, err := sql.Open("pgx", cfg.DBDSN)
	if err != nil {
		return nil, fmt.Errorf("open pgx DB: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	return db, nil
}

func (a *app) register(mux *http.ServeMux) {
	mux.HandleFunc("/health", a.serveHealth)
	mux.HandleFunc("/health/ready", a.serveReady)
	mux.HandleFunc("/health/active", a.serveActive)
	mux.HandleFunc("/health/ha", a.serveHA)
}

func (a *app) runLeaseLoop(ctx context.Context) {
	for {
		a.leaseTick(ctx)
		wait := a.cfg.AcquireInterval
		if a.snapshot(time.Now().UTC()).Active {
			wait = a.cfg.RenewInterval
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (a *app) leaseTick(ctx context.Context) {
	opCtx, cancel := context.WithTimeout(ctx, a.cfg.OperationTimeout)
	defer cancel()

	if err := a.ensureSchema(opCtx); err != nil {
		a.demote("schema check failed: " + err.Error())
		a.reconnectAfterError(err)
		return
	}

	snapshot := a.snapshot(time.Now().UTC())
	if snapshot.Active {
		lease, err := a.renewLease(opCtx, snapshot.LeaseEpoch)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				a.demote("lost active lease")
				return
			}
			a.demote("lease renewal failed: " + err.Error())
			a.reconnectAfterError(err)
			return
		}
		a.markActive(lease)
		return
	}

	lease, err := a.acquireLease(opCtx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			a.notePassive("lease held by peer")
			return
		}
		a.notePassive("lease acquisition failed: " + err.Error())
		a.reconnectAfterError(err)
		return
	}
	a.markActive(lease)
}

func (a *app) runHeartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.heartbeatTick(ctx)
		}
	}
}

func (a *app) heartbeatTick(ctx context.Context) {
	snapshot := a.snapshot(time.Now().UTC())
	if !snapshot.Active {
		return
	}
	opCtx, cancel := context.WithTimeout(ctx, a.cfg.OperationTimeout)
	defer cancel()

	wroteAt, err := a.writeHeartbeat(opCtx, snapshot.LeaseEpoch)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			a.demote("heartbeat rejected by stale lease epoch")
			return
		}
		a.demote("heartbeat write failed: " + err.Error())
		a.reconnectAfterError(err)
		return
	}
	a.mu.Lock()
	a.lastHeartbeatAt = wroteAt.UTC()
	a.lastError = ""
	a.mu.Unlock()
}

func (a *app) ensureSchema(ctx context.Context) error {
	a.mu.RLock()
	ready := a.schemaReady
	a.mu.RUnlock()
	if ready {
		return nil
	}
	db := a.currentDB()
	_, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS ha_poc_runtime_lease (
	name text PRIMARY KEY,
	holder_id text NOT NULL,
	host_id text NOT NULL,
	lease_epoch bigint NOT NULL,
	expires_at timestamptz NOT NULL,
	updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS ha_poc_active_heartbeat (
	id bigserial PRIMARY KEY,
	holder_id text NOT NULL,
	host_id text NOT NULL,
	lease_epoch bigint NOT NULL,
	wrote_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ha_poc_active_heartbeat_wrote_at
	ON ha_poc_active_heartbeat (wrote_at DESC);
`)
	if err != nil {
		return fmt.Errorf("create HA POC schema: %w", err)
	}
	a.mu.Lock()
	a.schemaReady = true
	a.mu.Unlock()
	return nil
}

func (a *app) acquireLease(ctx context.Context) (leaseRow, error) {
	var lease leaseRow
	err := a.currentDB().QueryRowContext(ctx, `
WITH upsert AS (
	INSERT INTO ha_poc_runtime_lease (
		name,
		holder_id,
		host_id,
		lease_epoch,
		expires_at,
		updated_at
	)
	VALUES ($1, $2, $3, 1, now() + $4::interval, now())
	ON CONFLICT (name) DO UPDATE SET
		holder_id = EXCLUDED.holder_id,
		host_id = EXCLUDED.host_id,
		lease_epoch = CASE
			WHEN ha_poc_runtime_lease.holder_id = EXCLUDED.holder_id
				THEN ha_poc_runtime_lease.lease_epoch
			ELSE ha_poc_runtime_lease.lease_epoch + 1
		END,
		expires_at = EXCLUDED.expires_at,
		updated_at = now()
	WHERE ha_poc_runtime_lease.holder_id = EXCLUDED.holder_id
		OR ha_poc_runtime_lease.expires_at <= now()
	RETURNING name, holder_id, host_id, lease_epoch, expires_at, updated_at
)
SELECT name, holder_id, host_id, lease_epoch, expires_at, updated_at, now(), NOT pg_is_in_recovery()
FROM upsert;
`, a.cfg.LeaseName, a.holderID, a.cfg.HostID, intervalLiteral(a.cfg.LeaseTTL)).Scan(
		&lease.Name,
		&lease.HolderID,
		&lease.HostID,
		&lease.Epoch,
		&lease.ExpiresAt,
		&lease.UpdatedAt,
		&lease.DBNow,
		&lease.DBWritable,
	)
	if err != nil {
		return leaseRow{}, fmt.Errorf("acquire lease: %w", err)
	}
	return lease, nil
}

func (a *app) renewLease(ctx context.Context, epoch int64) (leaseRow, error) {
	var lease leaseRow
	err := a.currentDB().QueryRowContext(ctx, `
UPDATE ha_poc_runtime_lease
SET expires_at = now() + $4::interval,
	updated_at = now()
WHERE name = $1
	AND holder_id = $2
	AND lease_epoch = $3
	AND expires_at > now()
RETURNING name, holder_id, host_id, lease_epoch, expires_at, updated_at, now(), NOT pg_is_in_recovery();
`, a.cfg.LeaseName, a.holderID, epoch, intervalLiteral(a.cfg.LeaseTTL)).Scan(
		&lease.Name,
		&lease.HolderID,
		&lease.HostID,
		&lease.Epoch,
		&lease.ExpiresAt,
		&lease.UpdatedAt,
		&lease.DBNow,
		&lease.DBWritable,
	)
	if err != nil {
		return leaseRow{}, fmt.Errorf("renew lease: %w", err)
	}
	return lease, nil
}

func (a *app) writeHeartbeat(ctx context.Context, epoch int64) (time.Time, error) {
	var wroteAt time.Time
	err := a.currentDB().QueryRowContext(ctx, `
INSERT INTO ha_poc_active_heartbeat (holder_id, host_id, lease_epoch, wrote_at)
SELECT $2, $3, $4, now()
WHERE EXISTS (
	SELECT 1
	FROM ha_poc_runtime_lease
	WHERE name = $1
		AND holder_id = $2
		AND lease_epoch = $4
		AND expires_at > now()
)
RETURNING wrote_at;
`, a.cfg.LeaseName, a.holderID, a.cfg.HostID, epoch).Scan(&wroteAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("write heartbeat: %w", err)
	}
	return wroteAt, nil
}

func (a *app) getLease(ctx context.Context) (leaseRow, error) {
	var lease leaseRow
	err := a.currentDB().QueryRowContext(ctx, `
SELECT name, holder_id, host_id, lease_epoch, expires_at, updated_at, now(), NOT pg_is_in_recovery()
FROM ha_poc_runtime_lease
WHERE name = $1;
`, a.cfg.LeaseName).Scan(
		&lease.Name,
		&lease.HolderID,
		&lease.HostID,
		&lease.Epoch,
		&lease.ExpiresAt,
		&lease.UpdatedAt,
		&lease.DBNow,
		&lease.DBWritable,
	)
	if err != nil {
		return leaseRow{}, fmt.Errorf("get lease: %w", err)
	}
	return lease, nil
}

func (a *app) checkReady(ctx context.Context) (readiness, error) {
	var ready readiness
	err := a.currentDB().QueryRowContext(ctx, `
SELECT
	NOT pg_is_in_recovery(),
	concat(coalesce(inet_server_addr()::text, 'local'), ':', inet_server_port());
	`).Scan(&ready.DBWritable, &ready.DBAddress)
	if err != nil {
		return readiness{}, fmt.Errorf("check DB readiness: %w", err)
	}
	ready.Ready = true
	a.mu.Lock()
	a.lastDBReadyAt = time.Now().UTC()
	a.lastDBPrimary = ready.DBAddress
	a.mu.Unlock()
	return ready, nil
}

func (a *app) markActive(lease leaseRow) {
	now := time.Now().UTC()
	a.mu.Lock()
	if !a.active {
		a.activeSince = now
	}
	a.active = true
	a.leaseEpoch = lease.Epoch
	a.leaseExpiresAt = lease.ExpiresAt.UTC()
	a.lastRenewAt = now
	a.lastError = ""
	a.mu.Unlock()
}

func (a *app) demote(reason string) {
	a.mu.Lock()
	a.active = false
	a.activeSince = time.Time{}
	a.leaseEpoch = 0
	a.leaseExpiresAt = time.Time{}
	a.lastError = reason
	a.mu.Unlock()
}

func (a *app) notePassive(reason string) {
	a.mu.Lock()
	a.active = false
	a.activeSince = time.Time{}
	a.leaseEpoch = 0
	a.leaseExpiresAt = time.Time{}
	a.lastError = reason
	a.mu.Unlock()
}

func (a *app) snapshot(now time.Time) statusSnapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	s := statusSnapshot{
		HostID:          a.cfg.HostID,
		HolderID:        a.holderID,
		Active:          a.active,
		ActiveSince:     a.activeSince,
		LeaseName:       a.cfg.LeaseName,
		LeaseEpoch:      a.leaseEpoch,
		LeaseExpiresAt:  a.leaseExpiresAt,
		LastRenewAt:     a.lastRenewAt,
		LastHeartbeatAt: a.lastHeartbeatAt,
		LastDBReadyAt:   a.lastDBReadyAt,
		LastDBPrimary:   a.lastDBPrimary,
		LastError:       a.lastError,
		StartedAt:       a.startedAt,
	}
	s.ActiveHealthy = activeHealthy(s, now, a.cfg.LeaseTTL)
	return s
}

func activeHealthy(s statusSnapshot, now time.Time, ttl time.Duration) bool {
	return s.Active &&
		s.LeaseEpoch > 0 &&
		!s.LeaseExpiresAt.IsZero() &&
		now.Before(s.LeaseExpiresAt) &&
		!s.LastRenewAt.IsZero() &&
		now.Sub(s.LastRenewAt) <= ttl
}

func (a *app) serveHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"live":      true,
		"host_id":   a.cfg.HostID,
		"holder_id": a.holderID,
	})
}

func (a *app) serveReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.OperationTimeout)
	defer cancel()
	ready, err := a.checkReady(ctx)
	if err != nil {
		a.reconnectAfterError(err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ready": false,
			"error": err.Error(),
		})
		return
	}
	if !ready.DBWritable {
		ready.Ready = false
		writeJSON(w, http.StatusServiceUnavailable, ready)
		return
	}
	writeJSON(w, http.StatusOK, ready)
}

func (a *app) serveActive(w http.ResponseWriter, _ *http.Request) {
	snapshot := a.snapshot(time.Now().UTC())
	if !snapshot.ActiveHealthy {
		writeJSON(w, http.StatusServiceUnavailable, snapshot)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (a *app) serveHA(w http.ResponseWriter, r *http.Request) {
	if !a.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "missing HA status token"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.OperationTimeout)
	defer cancel()

	response := map[string]any{
		"app": a.snapshot(time.Now().UTC()),
		"config": map[string]any{
			"lease_ttl":               a.cfg.LeaseTTL.String(),
			"renew_interval":          a.cfg.RenewInterval.String(),
			"acquire_interval":        a.cfg.AcquireInterval.String(),
			"heartbeat_interval":      a.cfg.HeartbeatInterval.String(),
			"db_dsn":                  redactDSN(a.cfg.DBDSN),
			"db_dsn_multi_host":       dsnLooksMultiHost(a.cfg.DBDSN),
			"db_dsn_read_write_attrs": dsnHasReadWriteTarget(a.cfg.DBDSN),
		},
	}
	if ready, err := a.checkReady(ctx); err == nil {
		response["ready"] = ready
	} else {
		response["ready_error"] = err.Error()
	}
	if lease, err := a.getLease(ctx); err == nil {
		response["lease"] = lease
	} else if errors.Is(err, sql.ErrNoRows) {
		response["lease"] = nil
	} else {
		response["lease_error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *app) authorized(r *http.Request) bool {
	if a.cfg.StatusToken == "" {
		return true
	}
	return r.Header.Get("Authorization") == "Bearer "+a.cfg.StatusToken
}

func (a *app) currentDB() *sql.DB {
	a.dbMu.RLock()
	defer a.dbMu.RUnlock()
	return a.db
}

func (a *app) reconnectAfterError(err error) {
	slog.Warn("resetting DB pool after error", slog.Any("error", err))
	db, openErr := openDB(a.cfg)
	if openErr != nil {
		slog.Warn("failed to reopen DB pool", slog.Any("error", openErr))
		return
	}
	a.dbMu.Lock()
	old := a.db
	a.db = db
	a.dbMu.Unlock()
	_ = old.Close()
	a.mu.Lock()
	a.schemaReady = false
	a.mu.Unlock()
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Warn("write JSON response", slog.Any("error", err))
	}
}

func newHolderID(hostID string) (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hostID + "-" + hex.EncodeToString(b[:]), nil
}

func intervalLiteral(d time.Duration) string {
	return fmt.Sprintf("%.3f seconds", d.Seconds())
}

func envOrDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

func boolEnv(name string) bool {
	return boolEnvDefault(name, false)
}

func boolEnvDefault(name string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if raw == "" {
		return fallback
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func dsnHasReadWriteTarget(dsn string) bool {
	return strings.Contains(strings.ToLower(dsn), "target_session_attrs=read-write")
}

func dsnLooksMultiHost(dsn string) bool {
	if strings.Contains(dsn, "://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			return false
		}
		return strings.Contains(parsed.Host, ",")
	}
	fields := strings.Fields(dsn)
	for _, field := range fields {
		if strings.HasPrefix(field, "host=") && strings.Contains(strings.TrimPrefix(field, "host="), ",") {
			return true
		}
	}
	return false
}

func redactDSN(dsn string) string {
	if strings.Contains(dsn, "://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			return "<invalid dsn>"
		}
		if parsed.User != nil {
			username := parsed.User.Username()
			if username == "" {
				parsed.User = url.UserPassword("", "xxxxx")
			} else {
				parsed.User = url.UserPassword(username, "xxxxx")
			}
		}
		return parsed.String()
	}
	fields := strings.Fields(dsn)
	for i, field := range fields {
		if strings.HasPrefix(strings.ToLower(field), "password=") {
			fields[i] = "password=xxxxx"
		}
	}
	return strings.Join(fields, " ")
}
