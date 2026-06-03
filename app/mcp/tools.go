package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/devour-app/devour/app/dbinspect"
	"github.com/devour-app/devour/app/projects"
)

type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

func (s *Server) getToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_services",
			Description: "List all services with their current status and ports",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "start_service",
			Description: "Start a service by name (apache, nginx, mysql, postgresql)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Service name",
						"enum":        []string{"apache", "nginx", "mysql", "postgresql"},
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "stop_service",
			Description: "Stop a service by name",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Service name",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "restart_service",
			Description: "Restart a service by name",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Service name",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "switch_php",
			Description: "Switch PHP version globally or for a specific project",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"version": map[string]interface{}{
						"type":        "string",
						"description": "PHP version (e.g., 8.2, 8.3)",
					},
					"project": map[string]interface{}{
						"type":        "string",
						"description": "Project name (optional, for per-project switching)",
					},
				},
				"required": []string{"version"},
			},
		},
		{
			Name:        "list_projects",
			Description: "List all registered projects with domain, path, framework, and PHP version",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "create_project",
			Description: "Create a new project with vhost and DNS entry",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Project name",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Filesystem path to the project",
					},
					"domain": map[string]interface{}{
						"type":        "string",
						"description": "Domain (e.g., myapp.test). Auto-generated if omitted.",
					},
				},
				"required": []string{"name", "path"},
			},
		},
		{
			Name:        "get_logs",
			Description: "Get recent log lines for a service",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"service": map[string]interface{}{
						"type":        "string",
						"description": "Service name",
					},
					"lines": map[string]interface{}{
						"type":        "integer",
						"description": "Number of lines to return (default: 50)",
					},
				},
				"required": []string{"service"},
			},
		},
		{
			Name:        "list_site_logs",
			Description: "List which per-vhost log files exist on disk for a project (access, error, ssl_access, ssl_error), with size and last-modified time. Use this before get_site_logs to see what is available.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"project": map[string]interface{}{
						"type":        "string",
						"description": "Project name (as returned by list_projects)",
					},
				},
				"required": []string{"project"},
			},
		},
		{
			Name:        "get_site_logs",
			Description: "Read the last N lines of a per-vhost log file for a project. Useful for debugging website-specific HTTP errors, slow requests, PHP fatals, etc. Apache and Nginx both write to the same filename pattern <domain>_<kind>.log.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"project": map[string]interface{}{
						"type":        "string",
						"description": "Project name (as returned by list_projects)",
					},
					"kind": map[string]interface{}{
						"type":        "string",
						"description": "Which log stream to read",
						"enum":        []string{"access", "error", "ssl_access", "ssl_error"},
					},
					"lines": map[string]interface{}{
						"type":        "integer",
						"description": "Number of trailing lines to return (default: 100, capped by ~5MB read from end of file)",
					},
				},
				"required": []string{"project", "kind"},
			},
		},
		{
			Name:        "set_project_ssl",
			Description: "Toggle HTTPS on/off for a project. Generates a self-signed cert (mkcert) on enable, regenerates the vhost, and reloads the running web server. The lock icon in Hangar's Project list does the same thing - this is the MCP-callable version.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"project": map[string]interface{}{"type": "string", "description": "Project name (from list_projects)"},
					"enabled": map[string]interface{}{"type": "boolean", "description": "true to enable HTTPS, false to disable"},
				},
				"required": []string{"project", "enabled"},
			},
		},
		{
			Name:        "run_sql",
			Description: "Run an ad-hoc SQL statement against the local MySQL or PostgreSQL Hangar manages. Returns columns+rows for SELECT/SHOW/EXPLAIN (capped at 500 rows), or rows_affected for INSERT/UPDATE/DELETE. Local-only (127.0.0.1).",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"db_type":  map[string]interface{}{"type": "string", "enum": []string{"mysql", "postgresql"}},
					"database": map[string]interface{}{"type": "string", "description": "Database name (e.g. 'restoliv'). Optional - leave empty to run against the server's default db."},
					"sql":      map[string]interface{}{"type": "string", "description": "SQL to execute. Single statement preferred."},
				},
				"required": []string{"db_type", "sql"},
			},
		},
		{
			Name:        "inspect_database",
			Description: "Run Hangar's server-side database audit. Inspects schema (tables, columns, indexes, FKs, triggers), runs ~30 deterministic rules (orphan FKs, missing indexes, money-as-float, plaintext secrets, multi-tenancy gaps, etc.), and writes a runnable .sql migration file with one block per finding grouped by severity. Returns 'sql_path' (read this file to see the fixes), 'summary' (count by severity), and 'audit_prompt' (short instructions for presenting results to the user). Set 'include_schema': true to also receive the raw schema JSON.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"db_type":         map[string]interface{}{"type": "string", "enum": []string{"mysql", "postgresql"}},
					"database":        map[string]interface{}{"type": "string", "description": "Database name to inspect."},
					"project_context": map[string]interface{}{"type": "string", "description": "Optional one-line context (e.g. 'Laravel 11 e-commerce app, Stripe integration, multi-tenant') so the prompt can frame domain-specific issues."},
					"include_schema":  map[string]interface{}{"type": "boolean", "description": "If true, response also includes the full Schema JSON (default: false - the rule engine has already consumed it server-side)."},
				},
				"required": []string{"db_type", "database"},
			},
		},
		{
			Name:        "get_connection_string",
			Description: "Get database connection string for MySQL or PostgreSQL",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"db_type": map[string]interface{}{
						"type":        "string",
						"description": "Database type",
						"enum":        []string{"mysql", "postgresql"},
					},
				},
				"required": []string{"db_type"},
			},
		},
	}
}

func (s *Server) handleToolsList(req JSONRPCRequest) (interface{}, *RPCError) {
	return map[string]interface{}{
		"tools": s.getToolDefinitions(),
	}, nil
}

func (s *Server) handleToolsCall(req JSONRPCRequest) (interface{}, *RPCError) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params"}
	}

	switch params.Name {
	case "list_services":
		return s.toolListServices()
	case "start_service":
		return s.toolStartService(params.Arguments)
	case "stop_service":
		return s.toolStopService(params.Arguments)
	case "restart_service":
		return s.toolRestartService(params.Arguments)
	case "switch_php":
		return s.toolSwitchPHP(params.Arguments)
	case "list_projects":
		return s.toolListProjects()
	case "create_project":
		return s.toolCreateProject(params.Arguments)
	case "get_logs":
		return s.toolGetLogs(params.Arguments)
	case "list_site_logs":
		return s.toolListSiteLogs(params.Arguments)
	case "get_site_logs":
		return s.toolGetSiteLogs(params.Arguments)
	case "set_project_ssl":
		return s.toolSetProjectSSL(params.Arguments)
	case "run_sql":
		return s.toolRunSQL(params.Arguments)
	case "inspect_database":
		return s.toolInspectDatabase(params.Arguments)
	case "get_connection_string":
		return s.toolGetConnectionString(params.Arguments)
	default:
		return nil, &RPCError{Code: -32601, Message: fmt.Sprintf("Unknown tool: %s", params.Name)}
	}
}

func (s *Server) toolListServices() (interface{}, *RPCError) {
	statuses := s.serviceManager.AllStatuses()
	return toolResult(statuses), nil
}

func (s *Server) toolStartService(args json.RawMessage) (interface{}, *RPCError) {
	var a struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid arguments"}
	}
	if err := s.serviceManager.Start(a.Name); err != nil {
		return toolError(err.Error()), nil
	}
	return toolResult(fmt.Sprintf("Service %s started", a.Name)), nil
}

func (s *Server) toolStopService(args json.RawMessage) (interface{}, *RPCError) {
	var a struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid arguments"}
	}
	if err := s.serviceManager.Stop(a.Name); err != nil {
		return toolError(err.Error()), nil
	}
	return toolResult(fmt.Sprintf("Service %s stopped", a.Name)), nil
}

func (s *Server) toolRestartService(args json.RawMessage) (interface{}, *RPCError) {
	var a struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid arguments"}
	}
	if err := s.serviceManager.Restart(a.Name); err != nil {
		return toolError(err.Error()), nil
	}
	return toolResult(fmt.Sprintf("Service %s restarted", a.Name)), nil
}

func (s *Server) toolSwitchPHP(args json.RawMessage) (interface{}, *RPCError) {
	var a struct {
		Version string `json:"version"`
		Project string `json:"project"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid arguments"}
	}

	if a.Project != "" {
		if err := s.phpManager.SwitchForProject(a.Version, a.Project); err != nil {
			return toolError(err.Error()), nil
		}
		return toolResult(fmt.Sprintf("PHP %s set for project %s", a.Version, a.Project)), nil
	}

	if err := s.phpManager.SwitchGlobal(a.Version); err != nil {
		return toolError(err.Error()), nil
	}
	return toolResult(fmt.Sprintf("PHP switched globally to %s", a.Version)), nil
}

func (s *Server) toolListProjects() (interface{}, *RPCError) {
	projects := s.projectManager.List()
	return toolResult(projects), nil
}

func (s *Server) toolCreateProject(args json.RawMessage) (interface{}, *RPCError) {
	var a struct {
		Name   string `json:"name"`
		Path   string `json:"path"`
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid arguments"}
	}
	project, err := s.projectManager.Create(a.Name, a.Path, a.Domain)
	if err != nil {
		return toolError(err.Error()), nil
	}
	return toolResult(project), nil
}

func (s *Server) toolGetLogs(args json.RawMessage) (interface{}, *RPCError) {
	var a struct {
		Service string `json:"service"`
		Lines   int    `json:"lines"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid arguments"}
	}
	if a.Lines == 0 {
		a.Lines = 50
	}
	logs := s.logStore.Get(a.Service, a.Lines)
	return toolResult(logs), nil
}

func (s *Server) toolListSiteLogs(args json.RawMessage) (interface{}, *RPCError) {
	var a struct {
		Project string `json:"project"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid arguments"}
	}
	files, err := s.projectManager.ListSiteLogs(a.Project)
	if err != nil {
		return toolError(err.Error()), nil
	}
	return toolResult(files), nil
}

func (s *Server) toolGetSiteLogs(args json.RawMessage) (interface{}, *RPCError) {
	var a struct {
		Project string `json:"project"`
		Kind    string `json:"kind"`
		Lines   int    `json:"lines"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid arguments"}
	}
	lines, err := s.projectManager.ReadSiteLog(a.Project, projects.SiteLogKind(a.Kind), a.Lines)
	if err != nil {
		return toolError(err.Error()), nil
	}
	return toolResult(lines), nil
}

func (s *Server) toolGetConnectionString(args json.RawMessage) (interface{}, *RPCError) {
	var a struct {
		DBType string `json:"db_type"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid arguments"}
	}

	switch a.DBType {
	case "mysql":
		status, _ := s.serviceManager.Status("mysql")
		connStr := fmt.Sprintf("mysql://root@127.0.0.1:%d", status.Port)
		return toolResult(connStr), nil
	case "postgresql":
		status, _ := s.serviceManager.Status("postgresql")
		connStr := fmt.Sprintf("postgresql://postgres@127.0.0.1:%d/postgres", status.Port)
		return toolResult(connStr), nil
	default:
		return toolError(fmt.Sprintf("Unknown database type: %s", a.DBType)), nil
	}
}

// --- New tools: SSL toggle, ad-hoc SQL, schema inspection + audit prompt ---

func (s *Server) toolSetProjectSSL(args json.RawMessage) (interface{}, *RPCError) {
	var a struct {
		Project string `json:"project"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid arguments"}
	}
	p, err := s.projectManager.Get(a.Project)
	if err != nil {
		return toolError(err.Error()), nil
	}
	p.SSLEnabled = a.Enabled
	if err := s.projectManager.Update(a.Project, p); err != nil {
		return toolError(err.Error()), nil
	}
	// Re-link so cert generation + vhost regen + web-server reload happen.
	if err := s.projectManager.EnsureProjectsReady(); err != nil {
		return toolError(err.Error()), nil
	}
	state := "disabled"
	if a.Enabled {
		state = "enabled"
	}
	return toolResult(fmt.Sprintf("HTTPS %s for %s", state, a.Project)), nil
}

func (s *Server) toolRunSQL(args json.RawMessage) (interface{}, *RPCError) {
	var a struct {
		DBType   string `json:"db_type"`
		Database string `json:"database"`
		SQL      string `json:"sql"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid arguments"}
	}
	dsn, err := s.dsnFor(a.DBType, a.Database)
	if err != nil {
		return toolError(err.Error()), nil
	}
	res, err := dbinspect.RunQuery(a.DBType, dsn, a.SQL)
	if err != nil {
		return toolError(err.Error()), nil
	}
	return toolResult(res), nil
}

func (s *Server) toolInspectDatabase(args json.RawMessage) (interface{}, *RPCError) {
	var a struct {
		DBType         string `json:"db_type"`
		Database       string `json:"database"`
		ProjectContext string `json:"project_context"`
		IncludeSchema  bool   `json:"include_schema"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid arguments"}
	}
	if a.Database == "" {
		return toolError("database is required - call list databases first if you don't know which"), nil
	}
	dsn, err := s.dsnFor(a.DBType, a.Database)
	if err != nil {
		return toolError(err.Error()), nil
	}
	schema, err := dbinspect.Inspect(a.DBType, dsn, a.Database)
	if err != nil {
		return toolError(err.Error()), nil
	}

	// Resolve where to write the audit file. Default is <data>/audits.
	// Falls back to OS temp if Paths isn't wired (shouldn't happen in
	// normal startup but keeps tests / CLI usage from panicking).
	outputDir := ""
	if s.paths != nil {
		outputDir = filepath.Join(s.paths.DataPath(), "audits")
	} else {
		outputDir = filepath.Join(os.TempDir(), "hangar-audits")
	}

	// Empty dialect = auto-detect from db_type. AuditAndWrite reads
	// schema.DBType which dbinspect.Inspect just populated.
	sqlPath, findings, summary, err := dbinspect.AuditAndWrite(schema, "", outputDir)
	if err != nil {
		return toolError(err.Error()), nil
	}

	resp := map[string]interface{}{
		"sql_path":     sqlPath,
		"summary":      summary,
		"audit_prompt": dbinspect.BuildAuditSummaryPrompt(schema, summary, sqlPath, a.ProjectContext),
		"findings":     findings, // structured form for agents that want to render their own UI
	}
	if a.IncludeSchema {
		resp["schema"] = schema
	}
	return toolResult(resp), nil
}

// dsnFor builds the local DSN for one of Hangar's bundled DBs. Uses the
// configured port (3306/5432 default), default credentials match what the
// init scripts set up: root with no password on MySQL, postgres with no
// password on Postgres. Local-bind only; never expose externally.
func (s *Server) dsnFor(dbType, database string) (string, error) {
	if s.configStore == nil {
		return "", fmt.Errorf("config store not wired")
	}
	cfg, _ := s.configStore.GetAppConfig()
	switch dbType {
	case "mysql", "mariadb":
		port := cfg.MySQLPort
		if port == 0 {
			port = 3306
		}
		return dbinspect.BuildDSN("mysql", "127.0.0.1", port, "root", "", database), nil
	case "postgresql", "postgres", "pg":
		port := cfg.PostgreSQLPort
		if port == 0 {
			port = 5432
		}
		return dbinspect.BuildDSN("postgres", "127.0.0.1", port, "postgres", "", database), nil
	}
	return "", fmt.Errorf("unknown db type %q (want mysql or postgresql)", dbType)
}

func toolResult(data interface{}) map[string]interface{} {
	content, _ := json.Marshal(data)
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": string(content),
			},
		},
	}
}

func toolError(msg string) map[string]interface{} {
	return map[string]interface{}{
		"isError": true,
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": msg,
			},
		},
	}
}
