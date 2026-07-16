package modbussim

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerHandlesFC5AndFC6AndRecordsState(t *testing.T) {
	simulator, address := startServer(t)
	conn := dialServer(t, address)
	defer conn.Close()

	coilOn := requestFrame(1, 7, functionWriteSingleCoil, 9, 0xFF00)
	sendFrame(t, conn, coilOn)
	assert.Equal(t, coilOn, readFrame(t, conn))

	coilOff := requestFrame(2, 7, functionWriteSingleCoil, 9, 0x0000)
	sendFrame(t, conn, coilOff)
	assert.Equal(t, coilOff, readFrame(t, conn))

	register := requestFrame(3, 8, functionWriteSingleRegister, 12, 0x1234)
	sendFrame(t, conn, register)
	assert.Equal(t, register, readFrame(t, conn))

	coil, ok := simulator.Coil(7, 9)
	require.True(t, ok)
	assert.False(t, coil)

	value, ok := simulator.HoldingRegister(8, 12)
	require.True(t, ok)
	assert.Equal(t, uint16(0x1234), value)

	assert.Equal(t, []Write{
		{FunctionCode: functionWriteSingleCoil, UnitID: 7, Address: 9, Value: 0xFF00},
		{FunctionCode: functionWriteSingleCoil, UnitID: 7, Address: 9, Value: 0x0000},
		{FunctionCode: functionWriteSingleRegister, UnitID: 8, Address: 12, Value: 0x1234},
	}, simulator.Writes())
}

func TestServerReturnsModbusExceptionsForUnsupportedAndInvalidRequests(t *testing.T) {
	tests := []struct {
		name         string
		frame        []byte
		wantFunction byte
		wantCode     byte
	}{
		{
			name:         "unsupported function",
			frame:        requestFrame(10, 1, 15, 2, 1),
			wantFunction: 0x8F,
			wantCode:     exceptionIllegalFunction,
		},
		{
			name:         "invalid coil value",
			frame:        requestFrame(11, 1, functionWriteSingleCoil, 2, 1),
			wantFunction: 0x85,
			wantCode:     exceptionIllegalDataValue,
		},
		{
			name: "invalid PDU length",
			frame: rawFrame(
				12,
				0,
				1,
				[]byte{functionWriteSingleRegister, 0, 2},
			),
			wantFunction: 0x86,
			wantCode:     exceptionIllegalDataValue,
		},
		{
			name: "invalid protocol ID",
			frame: rawFrame(
				13,
				1,
				1,
				[]byte{functionWriteSingleRegister, 0, 2, 0, 1},
			),
			wantFunction: 0x86,
			wantCode:     exceptionIllegalDataValue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			simulator, address := startServer(t)
			conn := dialServer(t, address)
			defer conn.Close()

			sendFrame(t, conn, tt.frame)
			response := readFrame(t, conn)

			require.Len(t, response, 9)
			assert.Equal(t, binary.BigEndian.Uint16(tt.frame[0:2]), binary.BigEndian.Uint16(response[0:2]))
			assert.Equal(t, uint16(0), binary.BigEndian.Uint16(response[2:4]))
			assert.Equal(t, uint16(3), binary.BigEndian.Uint16(response[4:6]))
			assert.Equal(t, tt.frame[6], response[6])
			assert.Equal(t, tt.wantFunction, response[7])
			assert.Equal(t, tt.wantCode, response[8])
			assert.Empty(t, simulator.Writes())
		})
	}
}

func TestServerRejectsOversizedFrameWithoutReadingOrAllocatingBody(t *testing.T) {
	_, address := startServer(t)
	conn := dialServer(t, address)
	defer conn.Close()

	header := make([]byte, mbapHeaderLength)
	binary.BigEndian.PutUint16(header[0:2], 21)
	binary.BigEndian.PutUint16(header[4:6], maxMBAPLength+1)
	header[6] = 4
	sendFrame(t, conn, header)

	response := readFrame(t, conn)
	require.Len(t, response, 9)
	assert.Equal(t, byte(0x80), response[7])
	assert.Equal(t, byte(exceptionIllegalDataValue), response[8])

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	_, err := conn.Read(make([]byte, 1))
	require.Error(t, err)
	assert.ErrorIs(t, err, io.EOF)
}

func TestServerHandlesConcurrentConnections(t *testing.T) {
	simulator, address := startServer(t)

	var wg sync.WaitGroup
	for unitID := byte(1); unitID <= 16; unitID++ {
		wg.Add(1)
		go func(unitID byte) {
			defer wg.Done()
			conn, err := net.DialTimeout("tcp", address, time.Second)
			assert.NoError(t, err)
			if err != nil {
				return
			}
			defer conn.Close()

			frame := requestFrame(
				uint16(unitID),
				unitID,
				functionWriteSingleRegister,
				uint16(100)+uint16(unitID),
				uint16(200)+uint16(unitID),
			)
			_, err = conn.Write(frame)
			assert.NoError(t, err)
			if err != nil {
				return
			}
			response, err := readFrameFrom(conn)
			assert.NoError(t, err)
			assert.Equal(t, frame, response)
		}(unitID)
	}
	wg.Wait()

	for unitID := byte(1); unitID <= 16; unitID++ {
		value, ok := simulator.HoldingRegister(unitID, uint16(100)+uint16(unitID))
		assert.True(t, ok)
		assert.Equal(t, uint16(200)+uint16(unitID), value)
	}
	assert.Len(t, simulator.Writes(), 16)
}

func TestServerClosesIdleConnectionAfterReadDeadline(t *testing.T) {
	_, address := startServerWithTimeouts(t, 30*time.Millisecond, time.Second)
	conn := dialServer(t, address)
	defer conn.Close()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	_, err := conn.Read(make([]byte, 1))
	require.Error(t, err)
	assert.ErrorIs(t, err, io.EOF)
}

func TestServerBoundsWriteHistory(t *testing.T) {
	simulator := New(WithLogger(slog.New(slog.DiscardHandler)))

	for value := range uint16(maxRecordedWrites + 5) {
		simulator.recordWrite(Write{
			FunctionCode: functionWriteSingleRegister,
			UnitID:       1,
			Address:      2,
			Value:        value,
		})
	}

	writes := simulator.Writes()
	require.Len(t, writes, maxRecordedWrites)
	assert.Equal(t, uint16(5), writes[0].Value)
	assert.Equal(t, uint16(maxRecordedWrites+4), writes[len(writes)-1].Value)
}

func TestServerBoundsTrackedPointState(t *testing.T) {
	simulator := New(WithLogger(slog.New(slog.DiscardHandler)))

	for address := range uint16(maxTrackedPoints) {
		require.True(t, simulator.recordWrite(Write{
			FunctionCode: functionWriteSingleRegister,
			UnitID:       1,
			Address:      address,
			Value:        address,
		}))
	}
	assert.False(t, simulator.recordWrite(Write{
		FunctionCode: functionWriteSingleRegister,
		UnitID:       2,
		Address:      1,
		Value:        1,
	}))
	assert.True(t, simulator.recordWrite(Write{
		FunctionCode: functionWriteSingleRegister,
		UnitID:       1,
		Address:      1,
		Value:        99,
	}))
}

func startServer(t *testing.T) (*Server, string) {
	t.Helper()
	return startServerWithTimeouts(t, time.Second, time.Second)
}

func startServerWithTimeouts(t *testing.T, readTimeout, writeTimeout time.Duration) (*Server, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	simulator := New(
		WithLogger(slog.New(slog.DiscardHandler)),
		WithTimeouts(readTimeout, writeTimeout),
	)
	done := make(chan error, 1)
	go func() {
		done <- simulator.Serve(listener)
	}()
	t.Cleanup(func() {
		require.NoError(t, simulator.Close())
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("simulator did not stop")
		}
	})
	return simulator, listener.Addr().String()
}

func dialServer(t *testing.T, address string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, time.Second)
	require.NoError(t, err)
	return conn
}

func requestFrame(transactionID uint16, unitID, functionCode byte, address, value uint16) []byte {
	pdu := make([]byte, 5)
	pdu[0] = functionCode
	binary.BigEndian.PutUint16(pdu[1:3], address)
	binary.BigEndian.PutUint16(pdu[3:5], value)
	return rawFrame(transactionID, 0, unitID, pdu)
}

func rawFrame(transactionID, protocolID uint16, unitID byte, pdu []byte) []byte {
	if len(pdu) > math.MaxUint16-1 {
		panic("test PDU is too large")
	}
	frame := make([]byte, mbapHeaderLength+len(pdu))
	binary.BigEndian.PutUint16(frame[0:2], transactionID)
	binary.BigEndian.PutUint16(frame[2:4], protocolID)
	// The guard above proves the MBAP length fits in uint16.
	binary.BigEndian.PutUint16(frame[4:6], uint16(1+len(pdu))) // #nosec G115
	frame[6] = unitID
	copy(frame[7:], pdu)
	return frame
}

func sendFrame(t *testing.T, conn net.Conn, frame []byte) {
	t.Helper()
	require.NoError(t, conn.SetWriteDeadline(time.Now().Add(time.Second)))
	_, err := conn.Write(frame)
	require.NoError(t, err)
}

func readFrame(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	frame, err := readFrameFrom(conn)
	require.NoError(t, err)
	return frame
}

func readFrameFrom(conn net.Conn) ([]byte, error) {
	header := make([]byte, mbapHeaderLength)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, fmt.Errorf("read response header: %w", err)
	}
	length := binary.BigEndian.Uint16(header[4:6])
	body := make([]byte, int(length)-1)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	frame := make([]byte, len(header)+len(body))
	copy(frame, header)
	copy(frame[len(header):], body)
	return frame, nil
}
