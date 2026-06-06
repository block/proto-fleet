package mqttclient

import (
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
	calls []subscribeCall
}

func (r *replayClient) Subscribe(topic string, qos byte, callback paho.MessageHandler) paho.Token {
	r.calls = append(r.calls, subscribeCall{
		topic:    topic,
		qos:      qos,
		callback: callback,
	})
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
	client.replaySubscriptions(replay)

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

func TestClientOptions_DurableOrderedSession(t *testing.T) {
	t.Parallel()

	opts := clientOptions("tcp://10.0.0.1:1883", nil, "user", "pass", "source|broker|topic", nil)

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
