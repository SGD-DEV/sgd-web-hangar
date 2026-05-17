package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/devour-app/devour/app/config"
	"github.com/devour-app/devour/app/logs"
	"github.com/devour-app/devour/app/php"
	"github.com/devour-app/devour/app/projects"
	"github.com/devour-app/devour/app/services"
)

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
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("mcp: server error: %v\n", err)
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

	s.httpServer.Shutdown(context.Background())
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
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

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
