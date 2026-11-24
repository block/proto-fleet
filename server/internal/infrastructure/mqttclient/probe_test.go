package mqttclient

import (
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/eclipse/paho.mqtt.golang/packets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientBrokerSessionPolicy(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		newClient    func() *Client
		cleanSession bool
	}{
		{name: "runtime retains session", newClient: New, cleanSession: false},
		{name: "probe discards session", newClient: NewProbe, cleanSession: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			t.Cleanup(func() { _ = listener.Close() })
			connected := make(chan *packets.ConnectPacket, 1)
			finished := make(chan error, 1)
			go func() {
				finished <- serveSessionPolicyProbe(listener, connected)
			}()
			host, port, err := net.SplitHostPort(listener.Addr().String())
			require.NoError(t, err)
			portNumber, err := strconv.ParseInt(port, 10, 32)
			require.NoError(t, err)
			client := tc.newClient()
			t.Cleanup(func() { client.Disconnect(time.Second) })
			require.NoError(t, client.Connect(t.Context(), host, int32(portNumber), transportTCP, "user", "pass", tc.name))
			packet := <-connected
			assert.Equal(t, tc.cleanSession, packet.CleanSession, "broker CONNECT session policy")
			require.NoError(t, client.Subscribe(t.Context(), "curtailment/probe", func([]byte, time.Time) {}))
			client.Disconnect(time.Second)
			select {
			case err := <-finished:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("broker did not receive probe disconnect")
			}
		})
	}
}

func serveSessionPolicyProbe(listener net.Listener, connected chan<- *packets.ConnectPacket) error {
	conn, err := listener.Accept()
	if err != nil {
		return fmt.Errorf("accept MQTT probe: %w", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("set MQTT probe deadline: %w", err)
	}
	packet, err := packets.ReadPacket(conn)
	if err != nil {
		return fmt.Errorf("read CONNECT: %w", err)
	}
	connect, ok := packet.(*packets.ConnectPacket)
	if !ok {
		return fmt.Errorf("got %T, want CONNECT", packet)
	}
	connected <- connect
	if err := packets.NewControlPacket(packets.Connack).Write(conn); err != nil {
		return fmt.Errorf("write CONNACK: %w", err)
	}
	packet, err = packets.ReadPacket(conn)
	if err != nil {
		return fmt.Errorf("read SUBSCRIBE: %w", err)
	}
	subscribe, ok := packet.(*packets.SubscribePacket)
	if !ok {
		return fmt.Errorf("got %T, want SUBSCRIBE", packet)
	}
	suback := &packets.SubackPacket{
		FixedHeader: packets.FixedHeader{MessageType: packets.Suback},
		MessageID:   subscribe.MessageID,
		ReturnCodes: []byte{subscribeQoS},
	}
	if err := suback.Write(conn); err != nil {
		return fmt.Errorf("write SUBACK: %w", err)
	}
	packet, err = packets.ReadPacket(conn)
	if err != nil {
		return fmt.Errorf("read DISCONNECT: %w", err)
	}
	if _, ok := packet.(*packets.DisconnectPacket); !ok {
		return fmt.Errorf("got %T, want DISCONNECT", packet)
	}
	return nil
}
