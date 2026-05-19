package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/devour-app/devour/app/config"
	"github.com/devour-app/devour/app/logs"
	"github.com/devour-app/devour/app/php"
	"github.com/devour-app/devour/app/projects"
	"github.com/devour-app/devour/app/services"
)

// isLocalRequest blocks DNS-rebinding attacks. The MCP server binds 127.0.0.1
// but the Host header can still be an attacker-controlled name that resolves
// to 127.0.0.1 via a malicious DNS server — a webpage on that domain then
// reaches the MCP server through the user's browser. Requiring Host to be
// localhost / 127.0.0.1 closes that path. Origin (when present) must also
// look local; for native MCP clients Origin is usually absent so we don't
// require it.
func isLocalRequest(r *http.Request) bool {
	host := r.Host
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	switch host {
	case "localhost", "127.0.0.1", "::1":
		// ok
	default:
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		o := strings.ToLower(origin)
		if !strings.HasPrefix(o, "http://localhost") &&
			!strings.HasPrefix(o, "https://localhost") &&
			!strings.HasPrefix(o, "http://127.0.0.1") &&
			!strings.HasPrefix(o, "https://127.0.0.1") {
			return false
		}
	}
	return true
}

const (
	MCPProtocolVersion = "2024-11-05"
	ServerName         = "hangar"
	ServerVersion      = "1.0.0"
)

type Server struct {
	serviceManager *services.Manager
	phpManager     *php.Manager
	projectManager *projects.Manager
	logStore       *logs.Store
	configStore    *config.Store // for resolving MySQL/Postgres ports for run_sql / inspect_database
	paths          config.Paths  // for writing audit SQL files under <data>/audits/
	httpServer     *http.Server
	mu             sync.Mutex
	running        bool
	port           int
}

func NewServer(svcMgr *services.Manager, phpMgr *php.Manager, projMgr *projects.Manager, logStore *logs.Store, configStore *config.Store, paths config.Paths) *Server {
	return &Server{
		serviceManager: svcMgr,
		phpManager:     phpMgr,
		projectManager: projMgr,
		logStore:       logStore,
		configStore:    configStore,
		paths:          paths,
		port:           3742,
	}
}

func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handleMCP)
	mux.HandleFunc("/sse", s.handleSSE)
	mux.HandleFunc("/health", s.handleHealth)

	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	// Bind synchronously so we can return a real error if port 3742 is
	// taken. The old code spawned ListenAndServe in a goroutine and set
	// running=true unconditionally — clients then got connection refused
	// while the UI happily reported "MCP running".
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("mcp: binding %s: %w", addr, err)
	}
	s.httpServer = &http.Server{Handler: mux}

	go func() {
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logStore.Add("mcp", fmt.Sprintf("server error: %v", err))
		}
	}()

	s.running = true
	return nil
}

func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.httpServer == nil {
		s.running = false
		return
	}

	// 5s ceiling. Without a timeout, a hung SSE client kept the shutdown
	// blocked forever and the Wails OnShutdown callback never returned, so
	// "Quit Hangar" appeared to do nothing.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.httpServer.Shutdown(ctx)
	s.running = false
}

func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"server":  ServerName,
		"version": ServerVersion,
	})
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if !isLocalRequest(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, nil, -32700, "Parse error")
		return
	}

	if req.JSONRPC != "2.0" {
		s.writeError(w, req.ID, -32600, "Invalid Request: jsonrpc must be 2.0")
		return
	}

	result, rpcErr := s.dispatch(req)
	if rpcErr != nil {
		s.writeErrorObj(w, req.ID, rpcErr)
		return
	}

	s.writeResult(w, req.ID, result)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	if !isLocalRequest(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Echo the request Origin instead of "*". With "*" any website could
	// open a long-lived stream to the MCP server through the user's
	// browser; combined with no auth on /mcp that was a real exfil path.
	// We've already verified Origin (if present) is localhost via
	// isLocalRequest, so echoing back is safe.
	if origin := r.Header.Get("Origin"); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n\n")
	flusher.Flush()

	<-r.Context().Done()
}

func (s *Server) dispatch(req JSONRPCRequest) (interface{}, *RPCError) {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	case "resources/list":
		return s.handleResourcesList(req)
	case "resources/read":
		return s.handleResourcesRead(req)
	default:
		return nil, &RPCError{Code: -32601, Message: fmt.Sprintf("Method not found: %s", req.Method)}
	}
}

func (s *Server) handleInitialize(req JSONRPCRequest) (interface{}, *RPCError) {
	return map[string]interface{}{
		"protocolVersion": MCPProtocolVersion,
		"capabilities": map[string]interface{}{
			"tools":     map[string]interface{}{},
			"resources": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    ServerName,
			"version": ServerVersion,
		},
	}, nil
}

func (s *Server) writeResult(w http.ResponseWriter, id interface{}, result interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func (s *Server) writeError(w http.ResponseWriter, id interface{}, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	})
}

func (s *Server) writeErrorObj(w http.ResponseWriter, id interface{}, rpcErr *RPCError) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   rpcErr,
	})
}
