package mqttclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
)

const subscribeQoS byte = 1
const maxPayloadBytes = 1024

const (
	transportTCP = "tcp"
	transportTLS = "tls"
)

// Client adapts Eclipse Paho to the curtailment MQTT ingest interface.
type Client struct {
	mu     sync.Mutex
	client paho.Client
}

var _ interface {
	Connect(ctx context.Context, host string, port int32, transport string, username, password string) error
	Subscribe(ctx context.Context, topic string, handler func(payload []byte, receivedAt time.Time)) error
	Disconnect(shutdownDeadline time.Duration)
} = (*Client)(nil)

func New() *Client {
	return &Client{}
}

func (c *Client) Connect(ctx context.Context, host string, port int32, transport string, username, password string) error {
	if port <= 0 {
		return fmt.Errorf("mqttclient: invalid broker port %d", port)
	}
	broker := net.JoinHostPort(host, strconv.Itoa(int(port)))
	brokerURL, tlsConfig, err := brokerOptions(host, broker, transport)
	if err != nil {
		return err
	}
	opts := paho.NewClientOptions().
		AddBroker(brokerURL).
		SetClientID(clientID()).
		SetUsername(username).
		SetPassword(password).
		SetAutoReconnect(true).
		SetResumeSubs(true).
		SetOrderMatters(false).
		SetProtocolVersion(4)
	if tlsConfig != nil {
		opts.SetTLSConfig(tlsConfig)
	}

	client := paho.NewClient(opts)
	if err := waitToken(ctx, client.Connect()); err != nil {
		client.Disconnect(0)
		return fmt.Errorf("mqttclient: connect %s: %w", broker, err)
	}

	c.mu.Lock()
	c.client = client
	c.mu.Unlock()
	return nil
}

func (c *Client) Subscribe(ctx context.Context, topic string, handler func(payload []byte, receivedAt time.Time)) error {
	if topic == "" {
		return errors.New("mqttclient: topic is required")
	}
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client == nil {
		return errors.New("mqttclient: subscribe before connect")
	}

	token := client.Subscribe(topic, subscribeQoS, func(_ paho.Client, msg paho.Message) {
		payload, ok := copyPayload(msg.Payload())
		if !ok {
			return
		}
		handler(payload, time.Now().UTC())
	})
	if err := waitToken(ctx, token); err != nil {
		return fmt.Errorf("mqttclient: subscribe %q: %w", topic, err)
	}
	return nil
}

func copyPayload(payload []byte) ([]byte, bool) {
	if len(payload) > maxPayloadBytes {
		return nil, false
	}
	return append([]byte(nil), payload...), true
}

func brokerOptions(host, broker, transport string) (string, *tls.Config, error) {
	switch transport {
	case "", transportTCP:
		return "tcp://" + broker, nil, nil
	case transportTLS:
		return "ssl://" + broker, &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: host,
		}, nil
	default:
		return "", nil, fmt.Errorf("mqttclient: unsupported broker transport %q", transport)
	}
}

func (c *Client) Disconnect(shutdownDeadline time.Duration) {
	c.mu.Lock()
	client := c.client
	c.client = nil
	c.mu.Unlock()
	if client == nil {
		return
	}
	client.Disconnect(quiesceMillis(shutdownDeadline))
}

func waitToken(ctx context.Context, token paho.Token) error {
	if token == nil {
		return errors.New("mqttclient: nil token")
	}
	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // ctx error surfaced verbatim; callers add context
	case <-token.Done():
		return token.Error() //nolint:wrapcheck // paho token error; callers add context
	}
}

func clientID() string {
	id := uuid.NewString()
	if len(id) > 12 {
		id = id[:12]
	}
	return "protofleet-" + id
}

func quiesceMillis(d time.Duration) uint {
	if d <= 0 {
		return 0
	}
	ms := d.Milliseconds()             // d > 0, so ms >= 0
	if uint64(ms) > uint64(^uint(0)) { //nolint:gosec // G115: ms >= 0; widened to uint64 for an in-range compare
		return ^uint(0)
	}
	return uint(ms) //nolint:gosec // G115: bounded above by the max-uint clamp
}
