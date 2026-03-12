package control

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/ggos3/sluice/internal/acl"
)

// Server listens on a Unix socket and handles runtime domain management commands.
type Server struct {
	socketPath string
	acl        *acl.Whitelist
	log        *slog.Logger
	listener   net.Listener
	wg         sync.WaitGroup
}

// NewServer creates a new control server.
func NewServer(socketPath string, whitelist *acl.Whitelist, log *slog.Logger) *Server {
	return &Server{
		socketPath: socketPath,
		acl:        whitelist,
		log:        log,
	}
}

// Start begins listening on the Unix socket.
func (s *Server) Start() error {
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0o755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}

	// Remove stale socket file
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen on socket: %w", err)
	}

	if err := os.Chmod(s.socketPath, 0o660); err != nil {
		listener.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}

	s.listener = listener

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.acceptLoop()
	}()

	s.log.Debug("control server started", "socket", s.socketPath)
	return nil
}

// Stop closes the listener and removes the socket file.
func (s *Server) Stop() error {
	if s.listener == nil {
		return nil
	}

	if err := s.listener.Close(); err != nil {
		return fmt.Errorf("close listener: %w", err)
	}

	s.wg.Wait()

	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove socket: %w", err)
	}

	s.log.Debug("control server stopped", "socket", s.socketPath)
	return nil
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return // listener closed
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(conn)
		}()
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		s.log.Debug("control: decode request failed", "error", err)
		return
	}

	resp := s.dispatch(req)

	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		s.log.Debug("control: encode response failed", "error", err)
	}
}

func (s *Server) dispatch(req Request) Response {
	switch req.Action {
	case "deny":
		return s.handleDeny(req)
	case "allow":
		return s.handleAllow(req)
	case "remove":
		return s.handleRemove(req)
	case "rules":
		return s.handleRules()
	default:
		return Response{OK: false, Error: fmt.Sprintf("unknown action: %s", req.Action)}
	}
}

func (s *Server) handleDeny(req Request) Response {
	if req.Domain == "" {
		return Response{OK: false, Error: "domain is required"}
	}

	s.acl.AddDeny(req.Domain)
	s.log.Info("domain denied", "domain", req.Domain)
	return Response{OK: true, Message: fmt.Sprintf("denied: %s", req.Domain)}
}

func (s *Server) handleAllow(req Request) Response {
	if req.Domain == "" {
		return Response{OK: false, Error: "domain is required"}
	}

	s.acl.AddAllow(req.Domain)
	s.log.Info("domain allowed", "domain", req.Domain)
	return Response{OK: true, Message: fmt.Sprintf("allowed: %s", req.Domain)}
}

func (s *Server) handleRemove(req Request) Response {
	if req.Domain == "" {
		return Response{OK: false, Error: "domain is required"}
	}

	found := s.acl.Remove(req.Domain)
	if !found {
		return Response{OK: true, Message: fmt.Sprintf("not found: %s", req.Domain)}
	}

	s.log.Info("domain rule removed", "domain", req.Domain)
	return Response{OK: true, Message: fmt.Sprintf("removed: %s", req.Domain)}
}

func (s *Server) handleRules() Response {
	var entries []RuleEntry

	for _, r := range s.acl.DynamicRules() {
		entries = append(entries, RuleEntry{
			Domain: r.Domain,
			Action: r.Action,
			Source: "runtime",
		})
	}

	for _, r := range s.acl.StaticRules() {
		entries = append(entries, RuleEntry{
			Domain: r.Domain,
			Action: r.Action,
			Source: "config",
		})
	}

	return Response{OK: true, Rules: entries}
}
