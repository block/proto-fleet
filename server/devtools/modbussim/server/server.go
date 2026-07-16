// Package modbussim implements a small development-only Modbus TCP listener.
package modbussim

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"slices"
	"sync"
	"time"
)

const (
	mbapHeaderLength = 7
	maxMBAPLength    = 254

	functionWriteSingleCoil     = 5
	functionWriteSingleRegister = 6

	exceptionIllegalFunction  = 1
	exceptionIllegalDataValue = 3
	exceptionServerFailure    = 4

	defaultReadTimeout  = 5 * time.Second
	defaultWriteTimeout = 5 * time.Second

	maxConcurrentConnections = 128
	maxRecordedWrites        = 1024
	maxTrackedPoints         = 4096
)

// Write is one accepted FC5 or FC6 request.
type Write struct {
	FunctionCode byte
	UnitID       byte
	Address      uint16
	Value        uint16
}

type stateKey struct {
	unitID  byte
	address uint16
}

// Option configures a Server.
type Option func(*Server)

// WithLogger sets the logger used for successful write records.
func WithLogger(logger *slog.Logger) Option {
	return func(server *Server) {
		if logger != nil {
			server.logger = logger
		}
	}
}

// WithTimeouts overrides per-request read and write deadlines.
func WithTimeouts(readTimeout, writeTimeout time.Duration) Option {
	return func(server *Server) {
		if readTimeout > 0 {
			server.readTimeout = readTimeout
		}
		if writeTimeout > 0 {
			server.writeTimeout = writeTimeout
		}
	}
}

// Server accepts FC5 and FC6 requests and records their resulting state.
type Server struct {
	logger       *slog.Logger
	readTimeout  time.Duration
	writeTimeout time.Duration

	stateMu          sync.RWMutex
	coils            map[stateKey]bool
	holdingRegisters map[stateKey]uint16
	writes           []Write
	nextWrite        int

	lifecycleMu     sync.Mutex
	listener        net.Listener
	connections     map[net.Conn]struct{}
	connectionSlots chan struct{}
	closed          bool
	wg              sync.WaitGroup
}

// New returns a ready-to-serve Modbus TCP simulator.
func New(options ...Option) *Server {
	server := &Server{
		logger:           slog.Default(),
		readTimeout:      defaultReadTimeout,
		writeTimeout:     defaultWriteTimeout,
		coils:            make(map[stateKey]bool),
		holdingRegisters: make(map[stateKey]uint16),
		connections:      make(map[net.Conn]struct{}),
		connectionSlots:  make(chan struct{}, maxConcurrentConnections),
	}
	for _, option := range options {
		option(server)
	}
	return server
}

// Serve accepts requests from listener until Close is called or listener fails.
func (s *Server) Serve(listener net.Listener) error {
	if listener == nil {
		return errors.New("Modbus simulator listener is required")
	}

	s.lifecycleMu.Lock()
	switch {
	case s.closed:
		s.lifecycleMu.Unlock()
		return errors.New("Modbus simulator is closed")
	case s.listener != nil:
		s.lifecycleMu.Unlock()
		return errors.New("Modbus simulator is already serving")
	default:
		s.listener = listener
		s.lifecycleMu.Unlock()
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			s.lifecycleMu.Lock()
			closed := s.closed
			s.lifecycleMu.Unlock()
			if closed || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept Modbus connection: %w", err)
		}

		select {
		case s.connectionSlots <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}

		s.lifecycleMu.Lock()
		if s.closed {
			s.lifecycleMu.Unlock()
			_ = conn.Close()
			<-s.connectionSlots
			continue
		}
		s.connections[conn] = struct{}{}
		s.wg.Add(1)
		s.lifecycleMu.Unlock()

		go s.serveConnection(conn)
	}
}

// Close stops accepting requests, closes active connections, and waits for all
// connection handlers to exit.
func (s *Server) Close() error {
	s.lifecycleMu.Lock()
	if s.closed {
		s.lifecycleMu.Unlock()
		s.wg.Wait()
		return nil
	}
	s.closed = true
	listener := s.listener
	connections := make([]net.Conn, 0, len(s.connections))
	for conn := range s.connections {
		connections = append(connections, conn)
	}
	s.lifecycleMu.Unlock()

	var closeErr error
	if listener != nil {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			closeErr = err
		}
	}
	for _, conn := range connections {
		_ = conn.Close()
	}
	s.wg.Wait()
	return closeErr
}

// Coil returns the latest FC5 state for a unit and wire address.
func (s *Server) Coil(unitID byte, address uint16) (bool, bool) {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	value, ok := s.coils[stateKey{unitID: unitID, address: address}]
	return value, ok
}

// HoldingRegister returns the latest FC6 value for a unit and wire address.
func (s *Server) HoldingRegister(unitID byte, address uint16) (uint16, bool) {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	value, ok := s.holdingRegisters[stateKey{unitID: unitID, address: address}]
	return value, ok
}

// Writes returns up to the most recent maxRecordedWrites accepted writes in
// arrival order.
func (s *Server) Writes() []Write {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	if len(s.writes) < maxRecordedWrites || s.nextWrite == 0 {
		return slices.Clone(s.writes)
	}
	return slices.Concat(s.writes[s.nextWrite:], s.writes[:s.nextWrite])
}

func (s *Server) serveConnection(conn net.Conn) {
	defer func() {
		_ = conn.Close()
		s.lifecycleMu.Lock()
		delete(s.connections, conn)
		s.lifecycleMu.Unlock()
		<-s.connectionSlots
		s.wg.Done()
	}()

	for {
		if err := conn.SetReadDeadline(time.Now().Add(s.readTimeout)); err != nil {
			return
		}

		var header [mbapHeaderLength]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			return
		}

		transactionID := binary.BigEndian.Uint16(header[0:2])
		protocolID := binary.BigEndian.Uint16(header[2:4])
		length := binary.BigEndian.Uint16(header[4:6])
		unitID := header[6]
		if length < 1 || length > maxMBAPLength {
			_ = s.writeException(conn, transactionID, unitID, 0, exceptionIllegalDataValue)
			return
		}

		pdu := make([]byte, int(length)-1)
		if _, err := io.ReadFull(conn, pdu); err != nil {
			return
		}

		var functionCode byte
		if len(pdu) > 0 {
			functionCode = pdu[0]
		}
		if protocolID != 0 || len(pdu) != 5 {
			if err := s.writeException(
				conn,
				transactionID,
				unitID,
				functionCode,
				exceptionIllegalDataValue,
			); err != nil {
				return
			}
			continue
		}

		address := binary.BigEndian.Uint16(pdu[1:3])
		value := binary.BigEndian.Uint16(pdu[3:5])
		write := Write{
			FunctionCode: functionCode,
			UnitID:       unitID,
			Address:      address,
			Value:        value,
		}
		switch functionCode {
		case functionWriteSingleCoil:
			if value != 0x0000 && value != 0xFF00 {
				if err := s.writeException(
					conn,
					transactionID,
					unitID,
					functionCode,
					exceptionIllegalDataValue,
				); err != nil {
					return
				}
				continue
			}
		case functionWriteSingleRegister:
		default:
			if err := s.writeException(
				conn,
				transactionID,
				unitID,
				functionCode,
				exceptionIllegalFunction,
			); err != nil {
				return
			}
			continue
		}
		if !s.recordWrite(write) {
			if err := s.writeException(
				conn,
				transactionID,
				unitID,
				functionCode,
				exceptionServerFailure,
			); err != nil {
				return
			}
			continue
		}

		response := make([]byte, mbapHeaderLength+len(pdu))
		copy(response, header[:])
		copy(response[mbapHeaderLength:], pdu)
		if err := s.writeFrame(conn, response); err != nil {
			return
		}
	}
}

func (s *Server) recordWrite(write Write) bool {
	key := stateKey{unitID: write.UnitID, address: write.Address}
	s.stateMu.Lock()
	pointExists := false
	switch write.FunctionCode {
	case functionWriteSingleCoil:
		_, pointExists = s.coils[key]
	case functionWriteSingleRegister:
		_, pointExists = s.holdingRegisters[key]
	}
	if !pointExists && len(s.coils)+len(s.holdingRegisters) >= maxTrackedPoints {
		s.stateMu.Unlock()
		return false
	}

	switch write.FunctionCode {
	case functionWriteSingleCoil:
		s.coils[key] = write.Value == 0xFF00
	case functionWriteSingleRegister:
		s.holdingRegisters[key] = write.Value
	}
	if len(s.writes) < maxRecordedWrites {
		s.writes = append(s.writes, write)
	} else {
		s.writes[s.nextWrite] = write
		s.nextWrite = (s.nextWrite + 1) % maxRecordedWrites
	}
	s.stateMu.Unlock()

	s.logger.Info(
		"modbus simulator write",
		slog.Uint64("unit_id", uint64(write.UnitID)),
		slog.Uint64("address", uint64(write.Address)),
		slog.Uint64("value", uint64(write.Value)),
	)
	return true
}

func (s *Server) writeException(
	conn net.Conn,
	transactionID uint16,
	unitID byte,
	functionCode byte,
	exceptionCode byte,
) error {
	response := make([]byte, 9)
	binary.BigEndian.PutUint16(response[0:2], transactionID)
	binary.BigEndian.PutUint16(response[4:6], 3)
	response[6] = unitID
	response[7] = functionCode | 0x80
	response[8] = exceptionCode
	return s.writeFrame(conn, response)
}

func (s *Server) writeFrame(conn net.Conn, frame []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(s.writeTimeout)); err != nil {
		return fmt.Errorf("set Modbus response deadline: %w", err)
	}
	for len(frame) > 0 {
		written, err := conn.Write(frame)
		if err != nil {
			return fmt.Errorf("write Modbus response: %w", err)
		}
		if written == 0 {
			return fmt.Errorf("write Modbus response: %w", io.ErrShortWrite)
		}
		frame = frame[written:]
	}
	return nil
}
