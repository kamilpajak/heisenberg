package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
)

// Server serves extracted artifacts over HTTP
type Server struct {
	listener net.Listener
	server   *http.Server
	dir      string
}

// Start creates a temp dir, extracts content, and starts HTTP server
func Start(content []byte, filename string) (*Server, error) {
	// Create temp directory
	dir, err := os.MkdirTemp("", "heisenberg-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Write content to file
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, content, 0644); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	// Find available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("failed to find port: %w", err)
	}

	srv := &Server{
		listener: listener,
		dir:      dir,
		server: &http.Server{
			Handler: http.FileServer(http.Dir(dir)),
		},
	}

	// Start server in background
	go srv.server.Serve(listener)

	return srv, nil
}

// URL returns the URL to access the served content
func (s *Server) URL(filename string) string {
	return fmt.Sprintf("http://%s/%s", s.listener.Addr().String(), filename)
}

// Stop shuts down the server and cleans up
func (s *Server) Stop() {
	s.server.Shutdown(context.Background())
	os.RemoveAll(s.dir)
}
