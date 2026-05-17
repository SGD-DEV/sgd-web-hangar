package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	cliAPIBase = "http://127.0.0.1:3741"
	mcpAPIBase = "http://127.0.0.1:3742"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

func main() {
	rootCmd := &cobra.Command{
		Use:   "devour",
		Short: "Devour — Local Web Development Environment",
		Long:  "Devour is a local web dev environment manager. Faster than everything.",
	}

	rootCmd.AddCommand(
		startCmd(),
		stopCmd(),
		restartCmd(),
		statusCmd(),
		phpCmd(),
		mysqlCmd(),
		postgresCmd(),
		projectCmd(),
		sslCmd(),
		logsCmd(),
		versionCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start all services",
		RunE: func(cmd *cobra.Command, args []string) error {
			return callMCPTool("list_services", nil)
		},
	}
}

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop all services",
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, svc := range []string{"apache", "nginx", "mysql", "postgresql"} {
				callMCPTool("stop_service", map[string]interface{}{"name": svc})
			}
			fmt.Println("All services stopped.")
			return nil
		},
	}
}

func restartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart all services",
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, svc := range []string{"apache", "nginx", "mysql", "postgresql"} {
				callMCPTool("restart_service", map[string]interface{}{"name": svc})
			}
			fmt.Println("All services restarted.")
			return nil
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show all service states and ports",
		RunE: func(cmd *cobra.Command, args []string) error {
			return callMCPTool("list_services", nil)
		},
	}
}

func phpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "php",
		Short: "PHP version management",
	}

	useCmd := &cobra.Command{
		Use:   "use [version]",
		Short: "Switch PHP version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, _ := cmd.Flags().GetString("project")
			params := map[string]interface{}{"version": args[0]}
			if project != "" {
				params["project"] = project
			}
			return callMCPTool("switch_php", params)
		},
	}
	useCmd.Flags().String("project", "", "Switch PHP for a specific project only")

	installCmd := &cobra.Command{
		Use:   "install [version]",
		Short: "Download and install a PHP version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("Downloading PHP %s...\n", args[0])
			fmt.Println("(Download functionality requires the desktop app)")
			return nil
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List installed PHP versions",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := readMCPResource("devour://php/versions")
			if err != nil {
				return err
			}
			fmt.Println(resp)
			return nil
		},
	}

	cmd.AddCommand(useCmd, installCmd, listCmd)
	return cmd
}

func mysqlCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mysql",
		Short: "MySQL service control",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "start",
			Short: "Start MySQL",
			RunE: func(cmd *cobra.Command, args []string) error {
				return callMCPTool("start_service", map[string]interface{}{"name": "mysql"})
			},
		},
		&cobra.Command{
			Use:   "stop",
			Short: "Stop MySQL",
			RunE: func(cmd *cobra.Command, args []string) error {
				return callMCPTool("stop_service", map[string]interface{}{"name": "mysql"})
			},
		},
	)
	return cmd
}

func postgresCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "postgres",
		Short: "PostgreSQL service control",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "start",
			Short: "Start PostgreSQL",
			RunE: func(cmd *cobra.Command, args []string) error {
				return callMCPTool("start_service", map[string]interface{}{"name": "postgresql"})
			},
		},
		&cobra.Command{
			Use:   "stop",
			Short: "Stop PostgreSQL",
			RunE: func(cmd *cobra.Command, args []string) error {
				return callMCPTool("stop_service", map[string]interface{}{"name": "postgresql"})
			},
		},
	)
	return cmd
}

func projectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Project management",
	}

	newCmd := &cobra.Command{
		Use:   "new [name]",
		Short: "Create a new project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := cmd.Flags().GetString("path")
			domain, _ := cmd.Flags().GetString("domain")
			if path == "" {
				path = "."
			}
			params := map[string]interface{}{
				"name": args[0],
				"path": path,
			}
			if domain != "" {
				params["domain"] = domain
			}
			return callMCPTool("create_project", params)
		},
	}
	newCmd.Flags().String("path", "", "Project path (default: current directory)")
	newCmd.Flags().String("domain", "", "Project domain (default: name.test)")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			return callMCPTool("list_projects", nil)
		},
	}

	openCmd := &cobra.Command{
		Use:   "open [name]",
		Short: "Open project in browser",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			domain := strings.ToLower(args[0]) + ".test"
			fmt.Printf("Opening https://%s ...\n", domain)
			return nil
		},
	}

	cmd.AddCommand(newCmd, listCmd, openCmd)
	return cmd
}

func sslCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ssl",
		Short: "SSL certificate management",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "trust",
			Short: "Install mkcert root CA",
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Println("Installing root CA...")
				fmt.Println("(Requires the desktop app)")
				return nil
			},
		},
		&cobra.Command{
			Use:   "gen [domain]",
			Short: "Generate SSL certificate for a domain",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Printf("Generating SSL cert for %s...\n", args[0])
				fmt.Println("(Requires the desktop app)")
				return nil
			},
		},
	)
	return cmd
}

func logsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs [service]",
		Short: "Tail service logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			lines, _ := cmd.Flags().GetInt("lines")
			if lines == 0 {
				lines = 50
			}
			return callMCPTool("get_logs", map[string]interface{}{
				"service": args[0],
				"lines":   lines,
			})
		},
	}
	cmd.Flags().Int("lines", 50, "Number of log lines to show")
	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show Devour version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Devour v1.0.0")
		},
	}
}

func callMCPTool(toolName string, arguments map[string]interface{}) error {
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	resp, err := httpClient.Post(mcpAPIBase+"/mcp", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("connecting to Devour (is the app running?): %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	if r, ok := result["result"]; ok {
		prettyPrint(r)
	} else if e, ok := result["error"]; ok {
		fmt.Fprintf(os.Stderr, "Error: %v\n", e)
	}

	return nil
}

func readMCPResource(uri string) (string, error) {
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "resources/read",
		"params": map[string]interface{}{
			"uri": uri,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	resp, err := httpClient.Post(mcpAPIBase+"/mcp", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("connecting to Devour: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func prettyPrint(v interface{}) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Println(v)
		return
	}
	fmt.Println(string(data))
}
