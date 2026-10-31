package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
)

type multiMinerHandler struct {
	miners map[[4]byte]*minerEntry
}

type minerEntry struct {
	once    sync.Once
	handler *http.ServeMux
}

func newMultiMinerHandler(path string) (*multiMinerHandler, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open miner addresses: %w", err)
	}
	defer file.Close()
	h := &multiMinerHandler{miners: make(map[[4]byte]*minerEntry)}
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		address := strings.TrimSpace(scanner.Text())
		if address == "" {
			continue
		}
		ip := net.ParseIP(address)
		if ip == nil || ip.To4() == nil || strings.Contains(address, ":") {
			return nil, fmt.Errorf("miner addresses line %d must be an IPv4 address", line)
		}
		h.miners[[4]byte(ip.To4())] = &minerEntry{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read miner addresses: %w", err)
	}
	if len(h.miners) == 0 {
		return nil, fmt.Errorf("miner address file is empty")
	}
	return h, nil
}

func (h *multiMinerHandler) destinationMux(r *http.Request) (*http.ServeMux, bool) {
	destinationIP, err := requestDestinationIP(r)
	if err != nil {
		return nil, false
	}
	entry, ok := h.miners[[4]byte(destinationIP)]
	if !ok {
		return nil, false
	}
	entry.once.Do(func() {
		serialNumber, macAddress := minerIdentityForIP(destinationIP)
		state := NewMinerState(serialNumber, macAddress)
		configureStartupAuthState(state)
		applyErrorConfig(state)
		state.IPAddress = destinationIP.String()
		entry.handler = newRESTMux(state, true)
	})
	return entry.handler, true
}

func (h *multiMinerHandler) Handler(r *http.Request) (http.Handler, string) {
	mux, ok := h.destinationMux(r)
	if !ok {
		return http.NotFoundHandler(), ""
	}
	return mux.Handler(r)
}

func (h *multiMinerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mux, ok := h.destinationMux(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	mux.ServeHTTP(w, r)
}

func requestDestinationIP(r *http.Request) (net.IP, error) {
	localAddress, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok || localAddress == nil {
		return nil, fmt.Errorf("request has no socket destination")
	}
	host, _, err := net.SplitHostPort(localAddress.String())
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host).To4()
	if ip == nil {
		return nil, fmt.Errorf("socket destination is not IPv4")
	}
	return ip, nil
}

func minerIdentityForIP(ip net.IP) (serialNumber, macAddress string) {
	ip = ip.To4()
	return fmt.Sprintf("PROTO-SIM-%02X%02X%02X%02X", ip[0], ip[1], ip[2], ip[3]),
		fmt.Sprintf("02:00:%02X:%02X:%02X:%02X", ip[0], ip[1], ip[2], ip[3])
}

func newRESTAPIHandler(state *MinerState) http.Handler {
	return newRESTMux(state, false)
}

func newRESTMux(state *MinerState, load bool) *http.ServeMux {
	mux := http.NewServeMux()
	api := NewRESTApiHandler(state)
	api.RegisterRoutes(mux)
	if load {
		mux.HandleFunc("/fake-api/v1/load/led", api.requireBearerAuth(loadLEDAudit(state)))
	}
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("OK")) })
	return mux
}
