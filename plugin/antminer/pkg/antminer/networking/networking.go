package networking

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
)

type IPAddress string

func (ip IPAddress) String() string {
	return string(ip)
}

type Port uint16

func (p Port) String() string {
	return strconv.Itoa(int(p))
}

type Protocol int

// Protocol constants for network communication with miners
const (
	// ProtocolHTTP is used for unencrypted web communication with miners
	ProtocolHTTP Protocol = iota
	// ProtocolHTTPS is used for secure encrypted web communication with miners
	ProtocolHTTPS
	// ProtocolTCP is used for direct socket connections with miners
	ProtocolTCP
)

func (p Protocol) String() string {
	switch p {
	case ProtocolHTTP:
		return "http"
	case ProtocolHTTPS:
		return "https"
	case ProtocolTCP:
		return "tcp"
	default:
		return "unknown"
	}
}

func ProtocolFromString(s string) (Protocol, error) {
	switch s {
	case "http":
		return ProtocolHTTP, nil
	case "https":
		return ProtocolHTTPS, nil
	case "tcp":
		return ProtocolTCP, nil
	default:
		return Protocol(-1), fmt.Errorf("unsupported protocol: %s", s)
	}
}

type ConnectionInfo struct {
	IPAddress IPAddress
	Port      Port
	Protocol  Protocol
}

func NewConnectionInfo(ipAddress string, port string, protocol Protocol) (*ConnectionInfo, error) {
	portInt, err := strconv.Atoi(port)
	if err != nil {
		return nil, fmt.Errorf("failed to convert port to int: %w", err)
	}
	if portInt < 0 || portInt > 65535 {
		return nil, fmt.Errorf("port out of range: %d", portInt)
	}

	return &ConnectionInfo{
		IPAddress: IPAddress(ipAddress),
		Port:      Port(portInt),
		Protocol:  protocol,
	}, nil
}

func (c ConnectionInfo) getHost() string {
	if c.Port == 0 {
		return string(c.IPAddress)
	}
	return net.JoinHostPort(string(c.IPAddress), c.Port.String())
}

func (c ConnectionInfo) GetURL() *url.URL {
	return &url.URL{
		Scheme: c.Protocol.String(),
		Host:   c.getHost(),
	}
}

func (c ConnectionInfo) GetHostPort() *url.URL {
	return &url.URL{Host: c.getHost()}
}
