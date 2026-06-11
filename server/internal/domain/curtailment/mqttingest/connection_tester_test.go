package mqttingest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingConnectionFactory struct {
	mu            sync.Mutex
	clients       []*recordingConnectionClient
	connectErrs   map[string]error
	subscribeErrs map[string]error
}

func newRecordingConnectionFactory() *recordingConnectionFactory {
	return &recordingConnectionFactory{
		connectErrs:   make(map[string]error),
		subscribeErrs: make(map[string]error),
	}
}

func (f *recordingConnectionFactory) newClient() MQTTClient {
	client := &recordingConnectionClient{factory: f}
	f.mu.Lock()
	f.clients = append(f.clients, client)
	f.mu.Unlock()
	return client
}

type recordingConnectionClient struct {
	factory *recordingConnectionFactory

	mu                 sync.Mutex
	host               string
	port               int32
	transport          string
	username           string
	password           string
	clientIdentity     string
	topic              string
	connectCalled      bool
	subscribeCalled    bool
	disconnectDeadline time.Duration
}

func (c *recordingConnectionClient) Connect(_ context.Context, host string, port int32, transport string, username, password, clientIdentity string) error {
	c.mu.Lock()
	c.host = host
	c.port = port
	c.transport = transport
	c.username = username
	c.password = password
	c.clientIdentity = clientIdentity
	c.connectCalled = true
	c.mu.Unlock()

	c.factory.mu.Lock()
	err := c.factory.connectErrs[host]
	c.factory.mu.Unlock()
	return err
}

func (c *recordingConnectionClient) Subscribe(_ context.Context, topic string, _ func(payload []byte, receivedAt time.Time)) error {
	c.mu.Lock()
	c.topic = topic
	c.subscribeCalled = true
	host := c.host
	c.mu.Unlock()

	c.factory.mu.Lock()
	err := c.factory.subscribeErrs[host]
	c.factory.mu.Unlock()
	return err
}

func (c *recordingConnectionClient) Disconnect(deadline time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.disconnectDeadline = deadline
}

func TestMQTTConnectionTester_TestConnectionConnectsAndSubscribesBothBrokers(t *testing.T) {
	t.Parallel()

	factory := newRecordingConnectionFactory()
	tester, err := NewMQTTConnectionTester(ConnectionTesterConfig{
		NewClient:        factory.newClient,
		Timeout:          time.Second,
		ShutdownDeadline: 50 * time.Millisecond,
	})
	require.NoError(t, err)

	result, err := tester.TestConnection(t.Context(), TestSourceConnectionRequest{
		Source:            testSourceConfig(),
		PlaintextPassword: "secret",
	})
	require.NoError(t, err)

	require.Len(t, result.Results, 2)
	assert.True(t, result.OK())
	assert.Equal(t, BrokerConnectionResult{
		Broker:     "10.0.0.1",
		Role:       BrokerPrimary,
		Connected:  true,
		Subscribed: true,
	}, result.Results[0])
	assert.Equal(t, BrokerConnectionResult{
		Broker:     "10.0.0.2",
		Role:       BrokerSecondary,
		Connected:  true,
		Subscribed: true,
	}, result.Results[1])

	factory.mu.Lock()
	clients := append([]*recordingConnectionClient(nil), factory.clients...)
	factory.mu.Unlock()
	require.Len(t, clients, 2)
	for _, client := range clients {
		client.mu.Lock()
		assert.True(t, client.connectCalled)
		assert.True(t, client.subscribeCalled)
		assert.Equal(t, int32(1883), client.port)
		assert.Equal(t, brokerTransportTCP, client.transport)
		assert.Equal(t, "operator", client.username)
		assert.Equal(t, "secret", client.password)
		assert.Equal(t, "maestro/curtailment", client.topic)
		assert.NotEmpty(t, client.clientIdentity)
		assert.Equal(t, 50*time.Millisecond, client.disconnectDeadline)
		client.mu.Unlock()
	}
}

func TestMQTTConnectionTester_TestConnectionReportsBrokerFailures(t *testing.T) {
	t.Parallel()

	factory := newRecordingConnectionFactory()
	factory.connectErrs["10.0.0.1"] = errors.New("dial refused")
	factory.subscribeErrs["10.0.0.2"] = errors.New("not authorized")
	tester, err := NewMQTTConnectionTester(ConnectionTesterConfig{
		NewClient: factory.newClient,
		Timeout:   time.Second,
	})
	require.NoError(t, err)

	result, err := tester.TestConnection(t.Context(), TestSourceConnectionRequest{
		Source:            testSourceConfig(),
		PlaintextPassword: "secret",
	})
	require.NoError(t, err)

	require.Len(t, result.Results, 2)
	assert.False(t, result.OK())
	assert.Equal(t, "dial refused", result.Results[0].Error)
	assert.False(t, result.Results[0].Connected)
	assert.False(t, result.Results[0].Subscribed)
	assert.Equal(t, "not authorized", result.Results[1].Error)
	assert.True(t, result.Results[1].Connected)
	assert.False(t, result.Results[1].Subscribed)
}
