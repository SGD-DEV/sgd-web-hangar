# Devour MCP Server

Devour exposes a Model Context Protocol (MCP) server on `localhost:3742`.

## Connecting

Any MCP-compatible AI tool (Claude, Cursor, Windsurf, VS Code Copilot) can connect to Devour's MCP server.

### Configuration

Add this to your MCP client configuration:

```json
{
  "mcpServers": {
    "devour": {
      "url": "http://localhost:3742/mcp"
    }
  }
}
```

## Available Tools

| Tool | Description |
|------|-------------|
| `list_services` | List all services with status and ports |
| `start_service(name)` | Start a service |
| `stop_service(name)` | Stop a service |
| `restart_service(name)` | Restart a service |
| `switch_php(version, project?)` | Switch PHP globally or per-project |
| `list_projects` | List all projects |
| `create_project(name, path, domain?)` | Create a new project |
| `get_logs(service, lines?)` | Get recent log lines |
| `get_connection_string(db_type)` | Get MySQL/PostgreSQL connection string |

## Available Resources

| URI | Description |
|-----|-------------|
| `devour://status` | JSON of all service states |
| `devour://projects` | JSON list of all projects |
| `devour://php/versions` | JSON list of installed PHP versions |

## Protocol

- **Protocol version:** 2024-11-05
- **Transport:** JSON-RPC 2.0 over HTTP POST
- **SSE endpoint:** `/sse` for real-time notifications
- **Health check:** `GET /health`
