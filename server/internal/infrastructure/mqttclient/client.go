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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

const subscribeQoS byte = 1
const maxPayloadBytes = 1024
const reconnectSubscribeTimeout = 10 * time.Second
const subscribeFailureCode byte = 0x80

const (
	transportTCP = "tcp"
	transportTLS = "tls"
)

// Client adapts Eclipse Paho to the curtailment MQTT ingest interface.
type Client struct {
	mu             sync.Mutex
	statusMu       sync.Mutex
	client         paho.Client
	subscriptions  map[string]paho.MessageHandler
	statusReporter func(connected bool, subscribed bool, err error)
	statusSequence atomic.Uint64
}

type subscriptionClient interface {
	Subscribe(topic string, qos byte, callback paho.MessageHandler) paho.Token
}

type connectionStateClient interface {
	IsConnectionOpen() bool
}

type reconnectClient interface {
	subscriptionClient
	connectionStateClient
}

type routeClient interface {
	AddRoute(topic string, callback paho.MessageHandler)
}

var _ interface {
	Connect(ctx context.Context, host string, port int32, transport string, username, password, clientIdentity string) error
	Subscribe(ctx context.Context, topic string, handler func(payload []byte, receivedAt time.Time)) error
	Disconnect(shutdownDeadline time.Duration)
} = (*Client)(nil)

var _ interface {
	SetRuntimeStatusReporter(reporter func(connected bool, subscribed bool, err error))
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
	initialOnConnectObserved := &atomic.Bool{}
	opts := clientOptions(brokerURL, tlsConfig, username, password, clientIdentity, func(client paho.Client) {
		if initialOnConnectObserved.CompareAndSwap(false, true) {
			return
		}
		if initialConnectComplete.Load() {
			c.reportReconnectStatus(ctx, client)
		}
	}, func(client paho.Client, err error) {
		c.reportConnectionLostStatus(client, err)
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
		if err := waitSubscribeToken(ctx, topic, client.Subscribe(topic, subscribeQoS, handler)); err != nil {
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

func (c *Client) SetRuntimeStatusReporter(reporter func(connected bool, subscribed bool, err error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statusReporter = reporter
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
	if err := waitSubscribeToken(ctx, topic, token); err != nil {
		return fmt.Errorf("mqttclient: subscribe %q: %w", topic, err)
	}
	return nil
}

func (c *Client) reportRuntimeStatus(connected bool, subscribed bool, err error) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	c.reportRuntimeStatusLocked(connected, subscribed, err)
}

func (c *Client) reportRuntimeStatusLocked(connected bool, subscribed bool, err error) {
	c.mu.Lock()
	reporter := c.statusReporter
	c.mu.Unlock()
	if reporter != nil {
		reporter(connected, subscribed, err)
	}
}

func (c *Client) nextStatusSequence() uint64 {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	return c.statusSequence.Add(1)
}

func (c *Client) reportRuntimeStatusForSequence(sequence uint64, connected bool, subscribed bool, err error) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	if c.statusSequence.Load() != sequence {
		return
	}
	c.reportRuntimeStatusLocked(connected, subscribed, err)
}

func (c *Client) reportConnectionLostStatus(client connectionStateClient, err error) {
	if client != nil && client.IsConnectionOpen() {
		return
	}
	sequence := c.nextStatusSequence()
	c.reportRuntimeStatusForSequence(sequence, false, false, normalizeConnectionLostError(err))
}

func (c *Client) reportReconnectStatus(ctx context.Context, client reconnectClient) {
	sequence := c.nextStatusSequence()
	replayCtx, cancel := context.WithTimeout(ctx, reconnectSubscribeTimeout)
	defer cancel()
	c.reportReconnectStatusForSequence(replayCtx, client, sequence)
}

func (c *Client) reportReconnectStatusForSequence(ctx context.Context, client reconnectClient, sequence uint64) {
	err := c.replaySubscriptions(ctx, client)
	if !client.IsConnectionOpen() {
		if err == nil {
			err = errors.New("mqttclient: connection lost during resubscribe")
		}
		c.reportRuntimeStatusForSequence(sequence, false, false, err)
		return
	}
	if err != nil {
		c.reportRuntimeStatusForSequence(sequence, true, false, err)
		return
	}
	c.reportRuntimeStatusForSequence(sequence, true, true, nil)
}

func (c *Client) replaySubscriptions(ctx context.Context, client subscriptionClient) error {
	for topic, handler := range c.subscriptionSnapshot() {
		if err := waitSubscribeToken(ctx, topic, client.Subscribe(topic, subscribeQoS, handler)); err != nil {
			return fmt.Errorf("mqttclient: resubscribe %q: %w", topic, err)
		}
	}
	return nil
}

func (c *Client) addRoutes(client routeClient) {
	for topic, handler := range c.subscriptionSnapshot() {
		client.AddRoute(normalizeTopicFilter(topic), handler)
	}
}

// normalizeTopicFilter strips shared-subscription prefixes the same way paho
// v1.5.1 does in Subscribe. Staged routes must use the normalized form:
// paho's router only strips $share (never $queue) when matching, and a route
// staged under the original filter would duplicate the normalized route paho
// adds on Subscribe, delivering each message twice. Degenerate filters with
// an empty remainder ("$queue/", "$share/<group>/") are kept as-is rather
// than registering an empty-topic route; Subscribe fails them normally.
func normalizeTopicFilter(topic string) string {
	if strings.HasPrefix(topic, "$share/") {
		parts := strings.SplitN(topic, "/", 3)
		if len(parts) == 3 && parts[2] != "" {
			return parts[2]
		}
		return topic
	}
	if rest, ok := strings.CutPrefix(topic, "$queue/"); ok && rest != "" {
		return rest
	}
	return topic
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
	onConnectionLost paho.ConnectionLostHandler,
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
	if onConnectionLost != nil {
		opts.SetConnectionLostHandler(onConnectionLost)
	}
	if tlsConfig != nil {
		opts.SetTLSConfig(tlsConfig)
	}
	return opts
}

func normalizeConnectionLostError(err error) error {
	if err != nil {
		return err
	}
	return errors.New("mqttclient: connection lost")
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

func waitSubscribeToken(ctx context.Context, topic string, token paho.Token) error {
	if err := waitToken(ctx, token); err != nil {
		return err
	}
	return validateSubscribeResult(topic, token)
}

func validateSubscribeResult(topic string, token paho.Token) error {
	resultToken, ok := token.(interface{ Result() map[string]byte })
	if !ok {
		return nil
	}
	result := resultToken.Result()
	qos, ok := result[topic]
	if !ok {
		// Paho v1.5.1 keys single-topic SUBACK results by the normalized
		// filter ($share/<group>/ and $queue/ prefixes stripped), not the
		// original one. The wrapper subscribes one topic at a time, so a
		// sole result entry is that subscription's outcome.
		if len(result) != 1 {
			return errors.New("SUBACK missing topic result")
		}
		for _, soleQoS := range result {
			qos = soleQoS
		}
	}
	if qos == subscribeFailureCode {
		return fmt.Errorf("SUBACK rejected subscription with code 0x%02x", qos)
	}
	if qos > 2 {
		return fmt.Errorf("SUBACK returned invalid QoS %d", qos)
	}
	return nil
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
