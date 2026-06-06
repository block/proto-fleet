package mqttclient

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

const subscribeQoS byte = 1
const maxPayloadBytes = 1024

const (
	transportTCP = "tcp"
	transportTLS = "tls"
)

// Client adapts Eclipse Paho to the curtailment MQTT ingest interface.
type Client struct {
	mu            sync.Mutex
	client        paho.Client
	subscriptions map[string]paho.MessageHandler
}

type subscriptionClient interface {
	Subscribe(topic string, qos byte, callback paho.MessageHandler) paho.Token
}

type routeClient interface {
	AddRoute(topic string, callback paho.MessageHandler)
}

var _ interface {
	Connect(ctx context.Context, host string, port int32, transport string, username, password, clientIdentity string) error
	Subscribe(ctx context.Context, topic string, handler func(payload []byte, receivedAt time.Time)) error
	Disconnect(shutdownDeadline time.Duration)
} = (*Client)(nil)

func New() *Client {
	return &Client{
		subscriptions: make(map[string]paho.MessageHandler),
	}
}

func (c *Client) Connect(ctx context.Context, host string, port int32, transport string, username, password, clientIdentity string) error {
	if port <= 0 {
		return fmt.Errorf("mqttclient: invalid broker port %d", port)
	}
	broker := net.JoinHostPort(host, strconv.Itoa(int(port)))
	brokerURL, tlsConfig, err := brokerOptions(host, broker, transport)
	if err != nil {
		return err
	}
	initialConnectComplete := &atomic.Bool{}
	opts := clientOptions(brokerURL, tlsConfig, username, password, clientIdentity, func(client paho.Client) {
		if initialConnectComplete.Load() {
			c.resubscribe(client)
		}
	})

	client := paho.NewClient(opts)
	c.addRoutes(client)
	if err := waitToken(ctx, client.Connect()); err != nil {
		client.Disconnect(0)
		return fmt.Errorf("mqttclient: connect %s: %w", broker, err)
	}

	c.mu.Lock()
	c.client = client
	c.mu.Unlock()
	for topic, handler := range c.subscriptionSnapshot() {
		if err := waitToken(ctx, client.Subscribe(topic, subscribeQoS, handler)); err != nil {
			client.Disconnect(0)
			c.mu.Lock()
			if c.client == client {
				c.client = nil
			}
			c.mu.Unlock()
			return fmt.Errorf("mqttclient: subscribe %q: %w", topic, err)
		}
	}
	initialConnectComplete.Store(true)
	return nil
}

func (c *Client) Subscribe(ctx context.Context, topic string, handler func(payload []byte, receivedAt time.Time)) error {
	if topic == "" {
		return errors.New("mqttclient: topic is required")
	}
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()

	messageHandler := func(_ paho.Client, msg paho.Message) {
		payload, ok := copyPayload(msg.Payload())
		if !ok {
			return
		}
		handler(payload, time.Now().UTC())
	}

	if client == nil {
		c.mu.Lock()
		if c.subscriptions == nil {
			c.subscriptions = make(map[string]paho.MessageHandler)
		}
		c.subscriptions[topic] = messageHandler
		c.mu.Unlock()
		return nil
	}

	c.mu.Lock()
	if c.subscriptions == nil {
		c.subscriptions = make(map[string]paho.MessageHandler)
	}
	c.subscriptions[topic] = messageHandler
	c.mu.Unlock()

	token := client.Subscribe(topic, subscribeQoS, messageHandler)
	if err := waitToken(ctx, token); err != nil {
		return fmt.Errorf("mqttclient: subscribe %q: %w", topic, err)
	}
	return nil
}

func (c *Client) resubscribe(client paho.Client) {
	c.replaySubscriptions(client)
}

func (c *Client) replaySubscriptions(client subscriptionClient) {
	for topic, handler := range c.subscriptionSnapshot() {
		client.Subscribe(topic, subscribeQoS, handler)
	}
}

func (c *Client) addRoutes(client routeClient) {
	for topic, handler := range c.subscriptionSnapshot() {
		client.AddRoute(topic, handler)
	}
}

func (c *Client) subscriptionSnapshot() map[string]paho.MessageHandler {
	c.mu.Lock()
	defer c.mu.Unlock()

	snapshot := make(map[string]paho.MessageHandler, len(c.subscriptions))
	for topic, handler := range c.subscriptions {
		snapshot[topic] = handler
	}
	return snapshot
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

func clientOptions(
	brokerURL string,
	tlsConfig *tls.Config,
	username string,
	password string,
	clientIdentity string,
	onConnect paho.OnConnectHandler,
) *paho.ClientOptions {
	opts := paho.NewClientOptions().
		AddBroker(brokerURL).
		SetClientID(clientID(clientIdentity)).
		SetUsername(username).
		SetPassword(password).
		SetAutoReconnect(true).
		SetResumeSubs(true).
		SetCleanSession(false).
		SetOnConnectHandler(onConnect).
		SetOrderMatters(true).
		SetProtocolVersion(4)
	if tlsConfig != nil {
		opts.SetTLSConfig(tlsConfig)
	}
	return opts
}

func (c *Client) Disconnect(shutdownDeadline time.Duration) {
	c.mu.Lock()
	client := c.client
	c.client = nil
	c.subscriptions = make(map[string]paho.MessageHandler)
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

func clientID(identity string) string {
	sum := sha256.Sum256([]byte(identity))
	return "protofleet-" + hex.EncodeToString(sum[:])[:12]
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
