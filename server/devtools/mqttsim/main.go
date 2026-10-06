// mqttsim provides a small web UI for driving MaestroOS-compatible MQTT
// curtailment targets during local development.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

const (
	defaultHTTPAddr     = ":4183"
	defaultTopic        = "maestro/target"
	defaultInterval     = 30 * time.Second
	defaultSettingsPath = "/settings/curtailment"
	qosAtLeastOnce      = 1
	wireTargetOff       = 0
	wireTargetOn        = 100
	maxLogEntries       = 100
	maxPayloadBytes     = 1024
)

type broker struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type config struct {
	httpAddr        string
	defaultTopic    string
	defaultInterval time.Duration
	brokers         []broker
	links           simulatorLinks
}

type simulatorLinks struct {
	CurtailmentSettingsURL string `json:"curtailment_settings_url,omitempty"`
}

type app struct {
	mu     sync.Mutex
	cfg    config
	state  simulatorState
	logs   []logEntry
	cancel context.CancelFunc
}

type simulatorState struct {
	Running           bool      `json:"running"`
	Target            string    `json:"target"`
	Topic             string    `json:"topic"`
	IntervalSeconds   int       `json:"interval_seconds"`
	Retain            bool      `json:"retain"`
	PrimaryEnabled    bool      `json:"primary_enabled"`
	SecondaryEnabled  bool      `json:"secondary_enabled"`
	TimestampOffset   int       `json:"timestamp_offset_seconds"`
	LastPublishedAt   time.Time `json:"last_published_at,omitempty"`
	LastPayload       string    `json:"last_payload,omitempty"`
	LastError         string    `json:"last_error,omitempty"`
	PublishedMessages int64     `json:"published_messages"`
}

type logEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

type statusResponse struct {
	Brokers []broker       `json:"brokers"`
	Links   simulatorLinks `json:"links,omitempty"`
	State   simulatorState `json:"state"`
	Logs    []logEntry     `json:"logs"`
}

type publishRequest struct {
	Target                 string `json:"target"`
	Topic                  string `json:"topic"`
	Retain                 bool   `json:"retain"`
	PrimaryEnabled         bool   `json:"primary_enabled"`
	SecondaryEnabled       bool   `json:"secondary_enabled"`
	TimestampOffsetSeconds int    `json:"timestamp_offset_seconds"`
	CustomPayload          string `json:"custom_payload"`
}

type loopRequest struct {
	Target                 string `json:"target"`
	Topic                  string `json:"topic"`
	IntervalSeconds        int    `json:"interval_seconds"`
	Retain                 bool   `json:"retain"`
	PrimaryEnabled         bool   `json:"primary_enabled"`
	SecondaryEnabled       bool   `json:"secondary_enabled"`
	TimestampOffsetSeconds int    `json:"timestamp_offset_seconds"`
}

type clearRequest struct {
	Topic            string `json:"topic"`
	PrimaryEnabled   bool   `json:"primary_enabled"`
	SecondaryEnabled bool   `json:"secondary_enabled"`
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("invalid mqtt simulator config", slog.Any("error", err))
		os.Exit(1)
	}
	a := newApp(cfg)
	mux := http.NewServeMux()
	a.register(mux)

	slog.Info("mqtt simulator starting",
		slog.String("addr", cfg.httpAddr),
		slog.String("topic", cfg.defaultTopic),
		slog.Any("brokers", cfg.brokers))
	server := &http.Server{
		Addr:              cfg.httpAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("mqtt simulator stopped", slog.Any("error", err))
		os.Exit(1)
	}
}

func loadConfig() (config, error) {
	cfg := config{
		httpAddr:        envOrDefault("HTTP_ADDR", defaultHTTPAddr),
		defaultTopic:    envOrDefault("MQTT_DEFAULT_TOPIC", defaultTopic),
		defaultInterval: defaultInterval,
		brokers: []broker{
			{Name: "primary", URL: "tcp://127.0.0.1:1883"},
			{Name: "secondary", URL: "tcp://127.0.0.1:1884"},
		},
	}
	if raw := strings.TrimSpace(os.Getenv("MQTT_DEFAULT_INTERVAL")); raw != "" {
		interval, err := time.ParseDuration(raw)
		if err != nil {
			return config{}, fmt.Errorf("parse MQTT_DEFAULT_INTERVAL: %w", err)
		}
		cfg.defaultInterval = interval
	}
	if raw := strings.TrimSpace(os.Getenv("MQTT_BROKERS")); raw != "" {
		brokers, err := parseBrokers(raw)
		if err != nil {
			return config{}, err
		}
		cfg.brokers = brokers
	}
	settingsURL, err := buildCurtailmentSettingsURL(
		strings.TrimSpace(os.Getenv("PROTO_FLEET_BASE_URL")),
		envOrDefault("PROTO_FLEET_CURTAILMENT_SETTINGS_PATH", defaultSettingsPath),
	)
	if err != nil {
		return config{}, err
	}
	cfg.links.CurtailmentSettingsURL = settingsURL
	if strings.TrimSpace(cfg.defaultTopic) == "" {
		return config{}, errors.New("MQTT_DEFAULT_TOPIC is required")
	}
	if cfg.defaultInterval <= 0 {
		return config{}, errors.New("MQTT_DEFAULT_INTERVAL must be positive")
	}
	if len(cfg.brokers) == 0 {
		return config{}, errors.New("at least one MQTT broker is required")
	}
	return cfg, nil
}

func buildCurtailmentSettingsURL(baseURL, settingsPath string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", nil
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse PROTO_FLEET_BASE_URL: %w", err)
	}
	if base.Scheme == "" || base.Host == "" {
		return "", errors.New("PROTO_FLEET_BASE_URL must be an absolute URL")
	}
	settingsPath = strings.TrimSpace(settingsPath)
	if settingsPath == "" {
		settingsPath = defaultSettingsPath
	}
	if !strings.HasPrefix(settingsPath, "/") {
		settingsPath = "/" + settingsPath
	}
	relative := &url.URL{Path: settingsPath}
	return base.ResolveReference(relative).String(), nil
}

func parseBrokers(raw string) ([]broker, error) {
	parts := strings.Split(raw, ",")
	out := make([]broker, 0, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name := fmt.Sprintf("broker-%d", i+1)
		url := part
		if before, after, ok := strings.Cut(part, "="); ok {
			name = strings.TrimSpace(before)
			url = strings.TrimSpace(after)
		}
		if name == "" || url == "" {
			return nil, fmt.Errorf("invalid MQTT_BROKERS entry %q", part)
		}
		if !strings.HasPrefix(url, "tcp://") && !strings.HasPrefix(url, "ssl://") {
			return nil, fmt.Errorf("broker %q URL must start with tcp:// or ssl://", name)
		}
		out = append(out, broker{Name: name, URL: url})
	}
	if len(out) == 0 {
		return nil, errors.New("MQTT_BROKERS did not contain any brokers")
	}
	return out, nil
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func newApp(cfg config) *app {
	return &app{
		cfg: cfg,
		state: simulatorState{
			Target:           "ON",
			Topic:            cfg.defaultTopic,
			IntervalSeconds:  int(cfg.defaultInterval / time.Second),
			Retain:           true,
			PrimaryEnabled:   true,
			SecondaryEnabled: true,
		},
	}
}

func (a *app) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /", a.handleIndex)
	mux.HandleFunc("GET /api/status", a.handleStatus)
	mux.HandleFunc("POST /api/publish", a.handlePublish)
	mux.HandleFunc("POST /api/loop/start", a.handleLoopStart)
	mux.HandleFunc("POST /api/loop/stop", a.handleLoopStop)
	mux.HandleFunc("POST /api/clear", a.handleClear)
}

func (a *app) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

func (a *app) handleStatus(w http.ResponseWriter, _ *http.Request) {
	a.writeStatus(w)
}

func (a *app) handlePublish(w http.ResponseWriter, r *http.Request) {
	var req publishRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	target, err := normalizeTarget(req.Target)
	if err != nil && strings.TrimSpace(req.CustomPayload) == "" {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := a.publishOnce(r.Context(), publishOptions{
		Target:                 target,
		Topic:                  topicOrDefault(req.Topic, a.cfg.defaultTopic),
		Retain:                 req.Retain,
		PrimaryEnabled:         req.PrimaryEnabled,
		SecondaryEnabled:       req.SecondaryEnabled,
		TimestampOffsetSeconds: req.TimestampOffsetSeconds,
		CustomPayload:          req.CustomPayload,
	}); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	a.writeStatus(w)
}

func (a *app) handleLoopStart(w http.ResponseWriter, r *http.Request) {
	var req loopRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	target, err := normalizeTarget(req.Target)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	interval := time.Duration(req.IntervalSeconds) * time.Second
	if interval <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("interval_seconds must be positive"))
		return
	}
	opts := publishOptions{
		Target:                 target,
		Topic:                  topicOrDefault(req.Topic, a.cfg.defaultTopic),
		Retain:                 req.Retain,
		PrimaryEnabled:         req.PrimaryEnabled,
		SecondaryEnabled:       req.SecondaryEnabled,
		TimestampOffsetSeconds: req.TimestampOffsetSeconds,
	}
	if len(a.selectedBrokers(opts.PrimaryEnabled, opts.SecondaryEnabled)) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("select at least one broker"))
		return
	}
	a.stopLoopIfRunning("stopped previous loop")
	if err := a.publishOnce(r.Context(), opts); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancel = cancel
	a.state.Running = true
	a.state.Target = target
	a.state.Topic = opts.Topic
	a.state.IntervalSeconds = req.IntervalSeconds
	a.state.Retain = opts.Retain
	a.state.PrimaryEnabled = opts.PrimaryEnabled
	a.state.SecondaryEnabled = opts.SecondaryEnabled
	a.state.TimestampOffset = opts.TimestampOffsetSeconds
	a.state.LastError = ""
	a.addLogLocked("info", fmt.Sprintf("started %s loop every %s", target, interval))
	a.mu.Unlock()

	go a.runLoop(ctx, interval, opts)
	a.writeStatus(w)
}

func (a *app) handleLoopStop(w http.ResponseWriter, _ *http.Request) {
	a.stopLoop("stopped loop")
	a.writeStatus(w)
}

func (a *app) handleClear(w http.ResponseWriter, r *http.Request) {
	var req clearRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	opts := publishOptions{
		Topic:            topicOrDefault(req.Topic, a.cfg.defaultTopic),
		Retain:           true,
		PrimaryEnabled:   req.PrimaryEnabled,
		SecondaryEnabled: req.SecondaryEnabled,
		CustomPayload:    "",
		ClearRetained:    true,
	}
	if err := a.publishOnce(r.Context(), opts); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	a.writeStatus(w)
}

func (a *app) runLoop(ctx context.Context, interval time.Duration, opts publishOptions) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.publishOnce(ctx, opts); err != nil {
				a.mu.Lock()
				a.state.LastError = err.Error()
				a.addLogLocked("error", err.Error())
				a.mu.Unlock()
			}
		}
	}
}

func (a *app) stopLoop(message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopLoopLocked() {
		a.addLogLocked("info", message)
	}
}

func (a *app) stopLoopIfRunning(message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopLoopLocked() {
		a.addLogLocked("info", message)
	}
}

func (a *app) stopLoopLocked() bool {
	if a.cancel == nil {
		return false
	}
	a.cancel()
	a.cancel = nil
	a.state.Running = false
	return true
}

type publishOptions struct {
	Target                 string
	Topic                  string
	Retain                 bool
	PrimaryEnabled         bool
	SecondaryEnabled       bool
	TimestampOffsetSeconds int
	CustomPayload          string
	ClearRetained          bool
}

func (a *app) publishOnce(ctx context.Context, opts publishOptions) error {
	topic := topicOrDefault(opts.Topic, a.cfg.defaultTopic)
	brokers := a.selectedBrokers(opts.PrimaryEnabled, opts.SecondaryEnabled)
	if len(brokers) == 0 {
		return errors.New("select at least one broker")
	}
	payload, err := buildPayload(opts)
	if err != nil {
		return err
	}
	for _, broker := range brokers {
		if err := publishToBroker(ctx, broker, topic, opts.Retain || opts.ClearRetained, payload); err != nil {
			return err
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Target = opts.Target
	a.state.Topic = topic
	a.state.Retain = opts.Retain
	a.state.PrimaryEnabled = opts.PrimaryEnabled
	a.state.SecondaryEnabled = opts.SecondaryEnabled
	a.state.TimestampOffset = opts.TimestampOffsetSeconds
	a.state.LastError = ""
	a.state.LastPayload = string(payload)
	a.state.LastPublishedAt = time.Now().UTC()
	a.state.PublishedMessages += int64(len(brokers))
	action := "published"
	if opts.ClearRetained {
		action = "cleared retained"
	}
	a.addLogLocked("info", fmt.Sprintf("%s topic=%q brokers=%d payload=%s", action, topic, len(brokers), payload))
	return nil
}

func buildPayload(opts publishOptions) ([]byte, error) {
	if opts.ClearRetained {
		return []byte{}, nil
	}
	if custom := strings.TrimSpace(opts.CustomPayload); custom != "" {
		if len(custom) > maxPayloadBytes {
			return nil, fmt.Errorf("custom payload exceeds %d bytes", maxPayloadBytes)
		}
		return []byte(custom), nil
	}
	target, err := normalizeTarget(opts.Target)
	if err != nil {
		return nil, err
	}
	wireTarget := wireTargetOn
	if target == "OFF" {
		wireTarget = wireTargetOff
	}
	body := struct {
		Target    int   `json:"target"`
		Timestamp int64 `json:"timestamp"`
	}{
		Target:    wireTarget,
		Timestamp: time.Now().UTC().Add(time.Duration(opts.TimestampOffsetSeconds) * time.Second).Unix(),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal target payload: %w", err)
	}
	return payload, nil
}

func publishToBroker(ctx context.Context, broker broker, topic string, retain bool, payload []byte) error {
	clientID := "proto-fleet-mqttsim-" + randomHex(6)
	opts := paho.NewClientOptions().
		AddBroker(broker.URL).
		SetClientID(clientID).
		SetConnectTimeout(5 * time.Second).
		SetWriteTimeout(5 * time.Second)
	client := paho.NewClient(opts)
	if err := waitToken(ctx, client.Connect(), 6*time.Second); err != nil {
		return fmt.Errorf("connect broker %s (%s): %w", broker.Name, broker.URL, err)
	}
	defer client.Disconnect(250)
	if err := waitToken(ctx, client.Publish(topic, qosAtLeastOnce, retain, payload), 6*time.Second); err != nil {
		return fmt.Errorf("publish broker %s topic %q: %w", broker.Name, topic, err)
	}
	return nil
}

func waitToken(ctx context.Context, token paho.Token, timeout time.Duration) error {
	if timeout <= 0 {
		return errors.New("MQTT token timeout must be positive")
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("context canceled while waiting for MQTT token: %w", err)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("timed out after %s", timeout)
		}
		wait := min(100*time.Millisecond, remaining)
		if !token.WaitTimeout(wait) {
			continue
		}
		if err := token.Error(); err != nil {
			return fmt.Errorf("MQTT token failed: %w", err)
		}
		return nil
	}
}

func (a *app) selectedBrokers(primary, secondary bool) []broker {
	out := make([]broker, 0, len(a.cfg.brokers))
	for _, broker := range a.cfg.brokers {
		switch strings.ToLower(broker.Name) {
		case "primary", "broker-1":
			if primary {
				out = append(out, broker)
			}
		case "secondary", "broker-2":
			if secondary {
				out = append(out, broker)
			}
		default:
			out = append(out, broker)
		}
	}
	return out
}

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	return nil
}

func (a *app) writeStatus(w http.ResponseWriter) {
	a.mu.Lock()
	resp := statusResponse{
		Brokers: append([]broker(nil), a.cfg.brokers...),
		Links:   a.cfg.links,
		State:   a.state,
		Logs:    append([]logEntry(nil), a.logs...),
	}
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, resp)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Warn("write json response", slog.Any("error", err))
	}
}

func (a *app) addLogLocked(level, message string) {
	a.logs = append([]logEntry{{
		Time:    time.Now().UTC(),
		Level:   level,
		Message: message,
	}}, a.logs...)
	if len(a.logs) > maxLogEntries {
		a.logs = a.logs[:maxLogEntries]
	}
}

func normalizeTarget(target string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(target)) {
	case "OFF":
		return "OFF", nil
	case "ON":
		return "ON", nil
	default:
		return "", fmt.Errorf("unsupported target %q; use ON or OFF", target)
	}
}

func topicOrDefault(topic, fallback string) string {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return fallback
	}
	return topic
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf)
}
