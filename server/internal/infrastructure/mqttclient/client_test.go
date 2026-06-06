package mqttclient

import (
	"strings"
	"testing"

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
