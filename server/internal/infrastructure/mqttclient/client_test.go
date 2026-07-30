package mqttclient

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

type subscribeCall struct {
	topic    string
	qos      byte
	callback paho.MessageHandler
}

type replayClient struct {
	calls             []subscribeCall
	token             paho.Token
	tokenErr          error
	connectionOpen    bool
	connectionOpenSet bool
}

func (r *replayClient) Subscribe(topic string, qos byte, callback paho.MessageHandler) paho.Token {
	r.calls = append(r.calls, subscribeCall{
		topic:    topic,
		qos:      qos,
		callback: callback,
	})
	if r.token != nil {
		return r.token
	}
	return completedToken{err: r.tokenErr}
}

func (r *replayClient) IsConnectionOpen() bool {
	if !r.connectionOpenSet {
		return true
	}
	return r.connectionOpen
}

func (r *replayClient) setConnectionOpen(open bool) {
	r.connectionOpen = open
	r.connectionOpenSet = true
}

type completedToken struct {
	err error
}

func (t completedToken) Wait() bool {
	return true
}

func (t completedToken) WaitTimeout(time.Duration) bool {
	return true
}

func (t completedToken) Done() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

func (t completedToken) Error() error {
	return t.err
}

type completedSubscribeToken struct {
	completedToken
	result map[string]byte
}

func (t completedSubscribeToken) Result() map[string]byte {
	return t.result
}

type pendingToken struct {
	done chan struct{}
}

func newPendingToken() pendingToken {
	return pendingToken{done: make(chan struct{})}
}

func (t pendingToken) Wait() bool {
	<-t.done
	return true
}

func (t pendingToken) WaitTimeout(timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-t.done:
		return true
	case <-timer.C:
		return false
	}
}

func (t pendingToken) Done() <-chan struct{} {
	return t.done
}

func (t pendingToken) Error() error {
	return nil
}

type routeCall struct {
	topic    string
	callback paho.MessageHandler
}

type routeRecorder struct {
	calls []routeCall
}

func (r *routeRecorder) AddRoute(topic string, callback paho.MessageHandler) {
	r.calls = append(r.calls, routeCall{topic: topic, callback: callback})
}

func TestBrokerOptions_TCP(t *testing.T) {
	t.Parallel()

	url, tlsConfig, err := brokerOptions("10.155.0.3", "10.155.0.3:1883", transportTCP)

	if err != nil {
		t.Fatalf("brokerOptions returned error: %v", err)
	}
	if url != "tcp://10.155.0.3:1883" {
		t.Fatalf("url = %q, want tcp URL", url)
	}
	if tlsConfig != nil {
		t.Fatal("tcp transport must not configure TLS")
	}
}

func TestBrokerOptions_TLS(t *testing.T) {
	t.Parallel()

	url, tlsConfig, err := brokerOptions("broker.example.com", "broker.example.com:8883", transportTLS)

	if err != nil {
		t.Fatalf("brokerOptions returned error: %v", err)
	}
	if url != "ssl://broker.example.com:8883" {
		t.Fatalf("url = %q, want ssl URL", url)
	}
	if tlsConfig == nil {
		t.Fatal("tls transport must configure TLS")
	}
	if tlsConfig.ServerName != "broker.example.com" {
		t.Fatalf("ServerName = %q, want broker host", tlsConfig.ServerName)
	}
}

func TestCopyPayloadRejectsOversizedPayload(t *testing.T) {
	t.Parallel()

	if _, ok := copyPayload([]byte(strings.Repeat("x", maxPayloadBytes+1))); ok {
		t.Fatal("oversized payload was accepted")
	}
}

func TestCopyPayloadCopiesAcceptedPayload(t *testing.T) {
	t.Parallel()

	in := []byte(`{"target":100,"timestamp":1778538975}`)
	got, ok := copyPayload(in)
	if !ok {
		t.Fatal("valid payload rejected")
	}
	got[0] = 'X'
	if in[0] == 'X' {
		t.Fatal("payload was not copied")
	}
}

func TestReplaySubscriptionsResubscribesStoredTopics(t *testing.T) {
	t.Parallel()

	client := New()
	handler := func(_ paho.Client, _ paho.Message) {}
	client.subscriptions["curtailment/source"] = handler

	replay := &replayClient{}
	if err := client.replaySubscriptions(t.Context(), replay); err != nil {
		t.Fatalf("replaySubscriptions returned error: %v", err)
	}

	if len(replay.calls) != 1 {
		t.Fatalf("replayed %d subscriptions, want 1", len(replay.calls))
	}
	call := replay.calls[0]
	if call.topic != "curtailment/source" {
		t.Fatalf("topic = %q, want curtailment/source", call.topic)
	}
	if call.qos != subscribeQoS {
		t.Fatalf("qos = %d, want %d", call.qos, subscribeQoS)
	}
	if call.callback == nil {
		t.Fatal("callback was not replayed")
	}
}

func TestReplaySubscriptionsReportsSubackFailure(t *testing.T) {
	t.Parallel()

	client := New()
	client.subscriptions["curtailment/source"] = func(_ paho.Client, _ paho.Message) {}

	replay := &replayClient{
		token: completedSubscribeToken{
			result: map[string]byte{"curtailment/source": subscribeFailureCode},
		},
	}
	err := client.replaySubscriptions(t.Context(), replay)

	if err == nil {
		t.Fatal("replaySubscriptions returned nil, want SUBACK failure")
	}
	if !strings.Contains(err.Error(), "SUBACK rejected subscription") {
		t.Fatalf("err = %v, want SUBACK rejected subscription", err)
	}
}

func TestValidateSubscribeResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		topic   string
		result  map[string]byte
		wantErr string
	}{
		{
			name:   "exact topic result succeeds",
			topic:  "maestro/target",
			result: map[string]byte{"maestro/target": 1},
		},
		{
			name:   "shared group filter succeeds with paho-normalized result key",
			topic:  "$share/group/maestro/target",
			result: map[string]byte{"maestro/target": 1},
		},
		{
			name:   "queue filter succeeds with paho-normalized result key",
			topic:  "$queue/maestro/target",
			result: map[string]byte{"maestro/target": 1},
		},
		{
			name:    "rejected SUBACK code fails",
			topic:   "maestro/target",
			result:  map[string]byte{"maestro/target": subscribeFailureCode},
			wantErr: "SUBACK rejected subscription",
		},
		{
			name:    "rejected SUBACK code fails via sole-entry fallback",
			topic:   "$share/group/maestro/target",
			result:  map[string]byte{"maestro/target": subscribeFailureCode},
			wantErr: "SUBACK rejected subscription",
		},
		{
			name:    "invalid QoS fails",
			topic:   "maestro/target",
			result:  map[string]byte{"maestro/target": 3},
			wantErr: "SUBACK returned invalid QoS",
		},
		{
			name:    "invalid QoS fails via sole-entry fallback",
			topic:   "$share/group/maestro/target",
			result:  map[string]byte{"maestro/target": 3},
			wantErr: "SUBACK returned invalid QoS",
		},
		{
			// Pins the deliberate leniency: the wrapper subscribes one topic
			// at a time, so a sole result entry is trusted even when its key
			// does not textually match the subscribed filter.
			name:   "sole entry accepted for non-shared topic mismatch",
			topic:  "maestro/target",
			result: map[string]byte{"other/topic": 1},
		},
		{
			name:    "empty result map fails",
			topic:   "maestro/target",
			result:  map[string]byte{},
			wantErr: "SUBACK missing topic result",
		},
		{
			name:  "multi-result map missing exact topic fails",
			topic: "maestro/target",
			result: map[string]byte{
				"other/topic":   1,
				"another/topic": 1,
			},
			wantErr: "SUBACK missing topic result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateSubscribeResult(tt.topic, completedSubscribeToken{result: tt.result})

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateSubscribeResult returned error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateSubscribeResult returned nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSubscribeResultIgnoresTokenWithoutResult(t *testing.T) {
	t.Parallel()

	if err := validateSubscribeResult("maestro/target", completedToken{}); err != nil {
		t.Fatalf("validateSubscribeResult returned error for plain token: %v", err)
	}
}

func TestSubscribeBeforeConnectStagesRoute(t *testing.T) {
	t.Parallel()

	client := New()
	err := client.Subscribe(t.Context(), "curtailment/source", func(_ []byte, _ time.Time) {})
	if err != nil {
		t.Fatalf("Subscribe before Connect returned error: %v", err)
	}

	recorder := &routeRecorder{}
	client.addRoutes(recorder)

	if len(recorder.calls) != 1 {
		t.Fatalf("routes added = %d, want 1", len(recorder.calls))
	}
	if recorder.calls[0].topic != "curtailment/source" {
		t.Fatalf("topic = %q, want curtailment/source", recorder.calls[0].topic)
	}
	if recorder.calls[0].callback == nil {
		t.Fatal("callback was not staged")
	}
}

func TestAddRoutesNormalizesSharedSubscriptionFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		topic string
		want  string
	}{
		{name: "share prefix stripped", topic: "$share/group/maestro/target", want: "maestro/target"},
		{name: "queue prefix stripped", topic: "$queue/maestro/target", want: "maestro/target"},
		{name: "plain topic unchanged", topic: "maestro/target", want: "maestro/target"},
		{name: "queue prefix with empty remainder unchanged", topic: "$queue/", want: "$queue/"},
		{name: "share prefix with empty remainder unchanged", topic: "$share/group/", want: "$share/group/"},
		{name: "share prefix missing topic segment unchanged", topic: "$share/group", want: "$share/group"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := New()
			if err := client.Subscribe(t.Context(), tt.topic, func(_ []byte, _ time.Time) {}); err != nil {
				t.Fatalf("Subscribe returned error: %v", err)
			}

			recorder := &routeRecorder{}
			client.addRoutes(recorder)

			if len(recorder.calls) != 1 {
				t.Fatalf("routes added = %d, want 1", len(recorder.calls))
			}
			if recorder.calls[0].topic != tt.want {
				t.Fatalf("route topic = %q, want %q", recorder.calls[0].topic, tt.want)
			}
		})
	}
}

func TestClientOptions_DurableOrderedSession(t *testing.T) {
	t.Parallel()

	opts := clientOptions("tcp://10.0.0.1:1883", nil, "user", "pass", "source|broker|topic", nil, nil)

	if opts.CleanSession {
		t.Fatal("CleanSession must be false so broker QoS1 queues survive fleetd restarts")
	}
	if !opts.ResumeSubs {
		t.Fatal("ResumeSubs must stay enabled for reconnect subscribe replay")
	}
	if !opts.Order {
		t.Fatal("OrderMatters must be true for curtailment signal ordering")
	}
	if opts.ClientID == "" || len(opts.ClientID) > 23 {
		t.Fatalf("ClientID = %q, want non-empty MQTT 3.1-compatible ID", opts.ClientID)
	}
	if opts.ClientID != clientID("source|broker|topic") {
		t.Fatalf("ClientID = %q, want deterministic clientID helper output", opts.ClientID)
	}
}

func TestClientOptions_RuntimeStatusCallbacks(t *testing.T) {
	t.Parallel()

	var connected bool
	var lostErr error
	opts := clientOptions(
		"tcp://10.0.0.1:1883",
		nil,
		"user",
		"pass",
		"source|broker|topic",
		func(paho.Client) {
			connected = true
		},
		func(_ paho.Client, err error) {
			lostErr = err
		},
	)

	if opts.OnConnect == nil {
		t.Fatal("OnConnect handler was not installed")
	}
	if opts.OnConnectionLost == nil {
		t.Fatal("OnConnectionLost handler was not installed")
	}

	opts.OnConnect(nil)
	if !connected {
		t.Fatal("OnConnect handler did not run")
	}

	wantErr := errors.New("broker lost")
	opts.OnConnectionLost(nil, wantErr)
	if !errors.Is(lostErr, wantErr) {
		t.Fatalf("lostErr = %v, want %v", lostErr, wantErr)
	}
}

type runtimeStatusCapture struct {
	reports    int
	connected  bool
	subscribed bool
	err        error
}

type runtimeStatusReport struct {
	connected  bool
	subscribed bool
	err        error
}

func captureRuntimeStatus(client *Client) *runtimeStatusCapture {
	capture := &runtimeStatusCapture{}
	client.SetRuntimeStatusReporter(func(connected bool, subscribed bool, err error) {
		capture.reports++
		capture.connected = connected
		capture.subscribed = subscribed
		capture.err = err
	})
	return capture
}

func reportReconnectStatusWithTestTimeout(t *testing.T, client *Client, replay reconnectClient, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	sequence := client.nextStatusSequence()
	client.reportReconnectStatusForSequence(ctx, replay, sequence)
}

func TestClientRuntimeStatusReporter(t *testing.T) {
	t.Parallel()

	client := New()
	status := captureRuntimeStatus(client)

	wantErr := errors.New("connection lost")
	client.reportRuntimeStatus(false, false, wantErr)

	if status.connected {
		t.Fatal("connected = true, want false")
	}
	if status.subscribed {
		t.Fatal("subscribed = true, want false")
	}
	if !errors.Is(status.err, wantErr) {
		t.Fatalf("err = %v, want %v", status.err, wantErr)
	}
}

func TestReportConnectionLostStatusSkipsAlreadyOpenClient(t *testing.T) {
	t.Parallel()

	client := New()
	status := captureRuntimeStatus(client)

	openClient := &replayClient{}
	openClient.setConnectionOpen(true)
	client.reportConnectionLostStatus(openClient, errors.New("stale connection loss"))

	if status.reports != 0 {
		t.Fatalf("runtime status reports = %d, want 0", status.reports)
	}
}

func TestReportReconnectStatusReportsSubscribedAfterReplaySuccess(t *testing.T) {
	t.Parallel()

	client := New()
	client.subscriptions["curtailment/source"] = func(_ paho.Client, _ paho.Message) {}
	status := captureRuntimeStatus(client)

	client.reportReconnectStatus(t.Context(), &replayClient{})

	if !status.connected {
		t.Fatal("connected = false, want true")
	}
	if !status.subscribed {
		t.Fatal("subscribed = false, want true")
	}
	if status.err != nil {
		t.Fatalf("err = %v, want nil", status.err)
	}
}

func TestReportReconnectStatusSuppressesStaleReplayReport(t *testing.T) {
	t.Parallel()

	client := New()
	client.subscriptions["curtailment/source"] = func(_ paho.Client, _ paho.Message) {}
	status := captureRuntimeStatus(client)

	staleSequence := client.nextStatusSequence()
	closedClient := &replayClient{}
	closedClient.setConnectionOpen(false)
	wantErr := errors.New("broker lost")
	client.reportConnectionLostStatus(closedClient, wantErr)
	if status.reports != 1 {
		t.Fatalf("runtime status reports = %d, want 1", status.reports)
	}
	if status.connected || status.subscribed {
		t.Fatalf("connected=%v subscribed=%v, want both false", status.connected, status.subscribed)
	}
	if !errors.Is(status.err, wantErr) {
		t.Fatalf("err = %v, want %v", status.err, wantErr)
	}

	client.reportReconnectStatusForSequence(t.Context(), &replayClient{}, staleSequence)
	if status.reports != 1 {
		t.Fatalf("stale replay report was not suppressed; reports = %d, want 1", status.reports)
	}
}

func TestReportRuntimeStatusForSequenceDoesNotOverwriteNewerDisconnect(t *testing.T) {
	t.Parallel()

	client := New()
	healthyReportEntered := make(chan struct{})
	allowHealthyReport := make(chan struct{})
	disconnectedReported := make(chan struct{})
	reports := make(chan runtimeStatusReport, 2)
	wantErr := errors.New("broker lost")

	client.SetRuntimeStatusReporter(func(connected bool, subscribed bool, err error) {
		if connected && subscribed {
			close(healthyReportEntered)
			<-allowHealthyReport
		}
		reports <- runtimeStatusReport{
			connected:  connected,
			subscribed: subscribed,
			err:        err,
		}
		if !connected && !subscribed {
			close(disconnectedReported)
		}
	})

	healthyReleased := false
	defer func() {
		if !healthyReleased {
			close(allowHealthyReport)
		}
	}()

	sequence := client.nextStatusSequence()
	healthyDone := make(chan struct{})
	go func() {
		defer close(healthyDone)
		client.reportRuntimeStatusForSequence(sequence, true, true, nil)
	}()

	select {
	case <-healthyReportEntered:
	case <-time.After(time.Second):
		t.Fatal("healthy report did not enter reporter")
	}

	closedClient := &replayClient{}
	closedClient.setConnectionOpen(false)
	disconnectDone := make(chan struct{})
	go func() {
		defer close(disconnectDone)
		client.reportConnectionLostStatus(closedClient, wantErr)
	}()

	select {
	case <-disconnectedReported:
	case <-time.After(50 * time.Millisecond):
	}

	close(allowHealthyReport)
	healthyReleased = true

	select {
	case <-healthyDone:
	case <-time.After(time.Second):
		t.Fatal("healthy report did not complete")
	}
	select {
	case <-disconnectDone:
	case <-time.After(time.Second):
		t.Fatal("disconnect report did not complete")
	}
	close(reports)

	var last runtimeStatusReport
	for report := range reports {
		last = report
	}
	if last.connected || last.subscribed {
		t.Fatalf("last report connected=%v subscribed=%v, want both false", last.connected, last.subscribed)
	}
	if !errors.Is(last.err, wantErr) {
		t.Fatalf("last err = %v, want %v", last.err, wantErr)
	}
}

func TestReportReconnectStatusReportsSubscribeReplayFailure(t *testing.T) {
	t.Parallel()

	client := New()
	client.subscriptions["curtailment/source"] = func(_ paho.Client, _ paho.Message) {}

	wantErr := errors.New("subscription rejected")
	replay := &replayClient{tokenErr: wantErr}
	status := captureRuntimeStatus(client)

	client.reportReconnectStatus(t.Context(), replay)

	if !status.connected {
		t.Fatal("connected = false, want true")
	}
	if status.subscribed {
		t.Fatal("subscribed = true, want false")
	}
	if !errors.Is(status.err, wantErr) {
		t.Fatalf("err = %v, want %v", status.err, wantErr)
	}
}

func TestReportReconnectStatusReportsDisconnectedWhenReplayTimeoutFindsClosedClient(t *testing.T) {
	t.Parallel()

	client := New()
	client.subscriptions["curtailment/source"] = func(_ paho.Client, _ paho.Message) {}

	replay := &replayClient{token: newPendingToken()}
	replay.setConnectionOpen(false)
	status := captureRuntimeStatus(client)

	reportReconnectStatusWithTestTimeout(t, client, replay, time.Millisecond)

	if status.connected {
		t.Fatal("connected = true, want false")
	}
	if status.subscribed {
		t.Fatal("subscribed = true, want false")
	}
	if !errors.Is(status.err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context deadline exceeded", status.err)
	}
}

func TestReportReconnectStatusReportsSubscribeReplayTimeout(t *testing.T) {
	t.Parallel()

	client := New()
	client.subscriptions["curtailment/source"] = func(_ paho.Client, _ paho.Message) {}

	replay := &replayClient{token: newPendingToken()}
	status := captureRuntimeStatus(client)

	reportReconnectStatusWithTestTimeout(t, client, replay, time.Millisecond)

	if !status.connected {
		t.Fatal("connected = false, want true")
	}
	if status.subscribed {
		t.Fatal("subscribed = true, want false")
	}
	if !errors.Is(status.err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context deadline exceeded", status.err)
	}
}

func TestClientID_DeterministicAndScoped(t *testing.T) {
	t.Parallel()

	left := clientID("source-a|broker-a|topic")
	if got := clientID("source-a|broker-a|topic"); got != left {
		t.Fatalf("clientID not deterministic: %q then %q", left, got)
	}
	if right := clientID("source-a|broker-b|topic"); right == left {
		t.Fatal("different broker identities must not share the same client ID")
	}
	if len(left) > 23 {
		t.Fatalf("clientID length = %d, want <= 23", len(left))
	}
}
