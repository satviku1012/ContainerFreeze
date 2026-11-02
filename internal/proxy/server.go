package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// Server buffers inbound HTTP traffic until the orchestrator confirms the container is ready.
type Server struct {
	Addr   string
	Logger *slog.Logger
	Client *Client
}

func NewServer(addr string, logger *slog.Logger, client *Client) *Server {
	return &Server{Addr: addr, Logger: logger, Client: client}
}

func (s *Server) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRequest)

	s.Logger.Info("proxy listening", "addr", s.Addr)
	return http.ListenAndServe(s.Addr, mux)
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	containerID := r.Header.Get("X-Container-ID")
	if containerID == "" {
		containerID = strings.TrimPrefix(r.URL.Path, "/")
	}
	targetIP := r.Header.Get("X-Target-IP")
	if targetIP == "" {
		targetIP = "127.0.0.1:8080"
	}

	if containerID == "" {
		http.Error(w, "missing container id", http.StatusBadRequest)
		return
	}
	if _, _, err := net.SplitHostPort(targetIP); err != nil {
		http.Error(w, fmt.Sprintf("invalid target address: %v", err), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if s.Logger != nil {
		s.Logger.Info("buffering request", "container_id", containerID, "target_ip", targetIP)
	}

	resp, err := s.Client.WakeContainer(ctx, containerID, targetIP)
	if err != nil {
		http.Error(w, fmt.Sprintf("wake failed: %v", err), http.StatusBadGateway)
		return
	}
	if !resp.Success {
		http.Error(w, resp.Message, http.StatusBadGateway)
		return
	}

	proxyTarget, err := url.Parse("http://" + targetIP)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid target: %v", err), http.StatusBadRequest)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(proxyTarget)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, fmt.Sprintf("proxy error: %v", err), http.StatusBadGateway)
	}
	if err := waitForTarget(ctx, targetIP); err != nil {
		http.Error(w, fmt.Sprintf("container did not become ready: %v", err), http.StatusGatewayTimeout)
		return
	}
	proxy.ServeHTTP(w, r)
}

func waitForTarget(ctx context.Context, target string) error {
	dialer := net.Dialer{Timeout: 250 * time.Millisecond}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		conn, err := dialer.DialContext(ctx, "tcp", target)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
