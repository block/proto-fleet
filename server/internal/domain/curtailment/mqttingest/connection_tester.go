package mqttingest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	defaultConnectionTestTimeout = 10 * time.Second
	connectionTestSourceName     = "connection-test"
)

// SourceConnectionTester verifies that a candidate MQTT source can connect
// and subscribe without persisting source settings or starting runtime ingest.
type SourceConnectionTester interface {
	TestConnection(ctx context.Context, req TestSourceConnectionRequest) (TestSourceConnectionResult, error)
}

type TestSourceConnectionRequest struct {
	Source            SourceConfig
	PlaintextPassword string
}

type TestSourceConnectionResult struct {
	Results []BrokerConnectionResult
}

func (r TestSourceConnectionResult) OK() bool {
	if len(r.Results) == 0 {
		return false
	}
	for _, result := range r.Results {
		if !result.Connected || !result.Subscribed || result.Error != "" {
			return false
		}
	}
	return true
}

type BrokerConnectionResult struct {
	Broker     string
	Role       BrokerRole
	Connected  bool
	Subscribed bool
	Error      string
}

type ConnectionTesterConfig struct {
	NewClient        MQTTClientFactory
	Timeout          time.Duration
	ShutdownDeadline time.Duration
}

type MQTTConnectionTester struct {
	newClient        MQTTClientFactory
	timeout          time.Duration
	shutdownDeadline time.Duration
}

func NewMQTTConnectionTester(cfg ConnectionTesterConfig) (*MQTTConnectionTester, error) {
	if cfg.NewClient == nil {
		return nil, errors.New("mqttingest: NewClient factory is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultConnectionTestTimeout
	}
	if cfg.ShutdownDeadline <= 0 {
		cfg.ShutdownDeadline = 10 * time.Second
	}
	return &MQTTConnectionTester{
		newClient:        cfg.NewClient,
		timeout:          cfg.Timeout,
		shutdownDeadline: cfg.ShutdownDeadline,
	}, nil
}

func (t *MQTTConnectionTester) TestConnection(ctx context.Context, req TestSourceConnectionRequest) (TestSourceConnectionResult, error) {
	source := normalizeSourceConfig(req.Source)
	primary, secondary, ok := ResolveBrokerRoles(source.BrokerPrimaryHost, source.BrokerSecondaryHost)
	if !ok {
		return TestSourceConnectionResult{}, errors.New("mqttingest: broker hosts must be distinct")
	}

	testCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	brokers := []struct {
		host string
		role BrokerRole
	}{
		{host: primary, role: BrokerPrimary},
		{host: secondary, role: BrokerSecondary},
	}
	results := make([]BrokerConnectionResult, len(brokers))
	var wg sync.WaitGroup
	for i, broker := range brokers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = t.testBroker(testCtx, source, req.PlaintextPassword, broker.host, broker.role)
		}()
	}
	wg.Wait()
	return TestSourceConnectionResult{Results: results}, nil
}

func (t *MQTTConnectionTester) testBroker(ctx context.Context, source SourceConfig, password, broker string, role BrokerRole) BrokerConnectionResult {
	result := BrokerConnectionResult{
		Broker: broker,
		Role:   role,
	}
	client := t.newClient()
	if client == nil {
		result.Error = "mqttingest: NewClient returned nil"
		return result
	}
	defer client.Disconnect(t.shutdownDeadline)

	if err := client.Connect(ctx, broker, source.BrokerPort, source.BrokerTransport, source.MQTTUsername, password, mqttConnectionTestIdentity(source, broker, role)); err != nil {
		result.Error = err.Error()
		return result
	}
	result.Connected = true
	if err := client.Subscribe(ctx, source.Topic, func([]byte, time.Time) {}); err != nil {
		result.Error = err.Error()
		return result
	}
	result.Subscribed = true
	return result
}

func mqttConnectionTestIdentity(source SourceConfig, broker string, role BrokerRole) string {
	return fmt.Sprintf("test|%d|%d|%s|%s|%d|%s|%d",
		source.OrganizationID,
		source.ServiceUserID,
		brokerRoleString(role),
		broker,
		source.BrokerPort,
		source.Topic,
		time.Now().UnixNano(),
	)
}

func brokerRoleString(role BrokerRole) string {
	switch role {
	case BrokerPrimary:
		return "primary"
	case BrokerSecondary:
		return "secondary"
	default:
		return "unknown"
	}
}
