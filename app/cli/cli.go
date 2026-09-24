// Package cli implements the terminal-driven side of Hangar:
// `hangar start mysql`, `hangar status`, `hangar daemon`, etc.
//
// It is invoked from main.go BEFORE Wails initialises - if os.Args
// looks like a CLI invocation, we run a command and exit; otherwise
// main.go falls through to the GUI path. Each command uses
// core.Bootstrap() directly so the terminal works without the GUI
// ever opening.
package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/devour-app/devour/app/core"
	"github.com/devour-app/devour/app/services"
	"github.com/spf13/cobra"
)

// Version is stamped at build time via -ldflags "-X github.com/devour-app/devour/app/cli.Version=v1.1.0".
// Defaults to "dev" so `go run .` works without flags.
var Version = "dev"

// IsCLIInvocation reports whether args (typically os.Args[1:]) should
// be handled by the CLI rather than Wails. We use the simple rule
// "any args at all means CLI" - GUI mode is exclusively the no-args
// case (plain double-click on hangar.exe, or `hangar` typed by hand).
//
// Unknown subcommands fall into the CLI path and cobra prints a clear
// "unknown command" error, which is the right UX: typing `hangar foo`
// should never silently launch the GUI.
func IsCLIInvocation(args []string) bool {
	if len(args) == 1 && args[0] == BackgroundFlag {
		return false
	}
	return len(args) > 0
}

// BackgroundFlag starts the normal GUI process with its window hidden (tray
// only). It is what the Windows autostart entry launches: one process owns
// the services, the tray and the window, so "Open" from the tray or a second
// launch from the Start menu just shows the window.
const BackgroundFlag = "--background"

// Run executes the CLI with args (typically os.Args[1:]) and returns
// the desired process exit code. Caller is responsible for calling
// os.Exit - we do not - so tests can assert on the code.
func Run(args []string) int {
	root := newRootCmd()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		// cobra has already printed the error message.
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "hangar",
		Short: "Hangar - local web development environment",
		Long: "Hangar is a local web development environment for Windows.\n" +
			"Run `hangar` with no arguments to launch the GUI, or use the\n" +
			"subcommands below to manage services from the terminal.",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(
		newVersionCmd(),
		newStartCmd(),
		newStopCmd(),
		newRestartCmd(),
		newStatusCmd(),
		newListCmd(),
		newServicesCmd(),
		newDaemonCmd(),
	)
	return root
}

// withCore runs fn against a freshly-bootstrapped Core suitable for a
// one-shot CLI command. Each command opens its own Core because the
// CLI process is short-lived; the daemon mode has its own longer-lived
// path. If another Hangar process holds the bbolt lock (the daemon, or
// the GUI), we degrade gracefully to a clear error.
func withCore(fn func(*core.Core) error) error {
	c, err := core.Bootstrap(core.DefaultCLIOptions(), core.StderrLogger)
	if err != nil {
		if _, ok := err.(core.ErrAlreadyRunning); ok {
			return fmt.Errorf("another Hangar process is running (GUI or daemon). " +
				"Close it first, or stop the daemon with `hangar daemon stop`")
		}
		return err
	}
	defer c.Shutdown()
	return fn(c)
}

// ---- version --------------------------------------------------------------

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print Hangar version and exit",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("hangar %s\n", Version)
			return nil
		},
	}
}

// ---- services lifecycle ----------------------------------------------------

// allServiceNames is the canonical order Hangar uses everywhere. Web
// servers first, then databases, then search, then mail - matches the
// Servers page ordering.
var allServiceNames = []string{
	"apache", "nginx", "caddy",
	"mysql", "postgresql", "mongodb",
	"meilisearch",
	"mailpit",
}

// expandTargets turns the user's positional args into a concrete list
// of service names. Empty args = every service; "all" / "*" too. An
// unknown name errors loudly rather than silently skipping.
func expandTargets(args []string) ([]string, error) {
	if len(args) == 0 {
		return allServiceNames, nil
	}
	known := map[string]bool{}
	for _, s := range allServiceNames {
		known[s] = true
	}
	var out []string
	for _, a := range args {
		a = strings.ToLower(strings.TrimSpace(a))
		switch a {
		case "all", "*":
			return allServiceNames, nil
		case "pg", "postgres":
			out = append(out, "postgresql")
			continue
		case "mongo":
			out = append(out, "mongodb")
			continue
		case "meili":
			out = append(out, "meilisearch")
			continue
		}
		if !known[a] {
			return nil, fmt.Errorf("unknown service %q (valid: %s)", a, strings.Join(allServiceNames, ", "))
		}
		out = append(out, a)
	}
	return out, nil
}

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start [service...]",
		Short: "Start one or more services (no args = start all)",
		Example: "  hangar start mysql\n" +
			"  hangar start apache mysql\n" +
			"  hangar start          # all services",
		RunE: func(cmd *cobra.Command, args []string) error {
			targets, err := expandTargets(args)
			if err != nil {
				return err
			}
			return withCore(func(c *core.Core) error {
				for _, name := range targets {
					fmt.Printf("Starting %s... ", name)
					if err := c.ServiceManager.Start(name); err != nil {
						fmt.Println("failed")
						fmt.Fprintf(os.Stderr, "  %v\n", err)
						continue
					}
					fmt.Println("ok")
				}
				return nil
			})
		},
	}
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop [service...]",
		Short: "Stop one or more services (no args = stop all)",
		Example: "  hangar stop mysql\n" +
			"  hangar stop          # all services",
		RunE: func(cmd *cobra.Command, args []string) error {
			targets, err := expandTargets(args)
			if err != nil {
				return err
			}
			return withCore(func(c *core.Core) error {
				for _, name := range targets {
					fmt.Printf("Stopping %s... ", name)
					if err := c.ServiceManager.Stop(name); err != nil {
						fmt.Println("failed")
						fmt.Fprintf(os.Stderr, "  %v\n", err)
						continue
					}
					fmt.Println("ok")
				}
				return nil
			})
		},
	}
}

func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart [service...]",
		Short: "Restart one or more services",
		RunE: func(cmd *cobra.Command, args []string) error {
			targets, err := expandTargets(args)
			if err != nil {
				return err
			}
			return withCore(func(c *core.Core) error {
				for _, name := range targets {
					fmt.Printf("Restarting %s... ", name)
					if err := c.ServiceManager.Restart(name); err != nil {
						fmt.Println("failed")
						fmt.Fprintf(os.Stderr, "  %v\n", err)
						continue
					}
					fmt.Println("ok")
				}
				return nil
			})
		},
	}
}

// ---- status / list ---------------------------------------------------------

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show status of every service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withCore(func(c *core.Core) error {
				printServiceTable(c.ServiceManager.AllStatuses())
				return nil
			})
		},
	}
}

// newListCmd is an alias for status when called without args; "list
// services" / "list projects" / "list php" routes to the right inner
// listing. Keeps the top-level surface small.
func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [services|projects|php]",
		Short: "List services, projects, or installed PHP versions",
		RunE: func(cmd *cobra.Command, args []string) error {
			what := "services"
			if len(args) > 0 {
				what = strings.ToLower(args[0])
			}
			return withCore(func(c *core.Core) error {
				switch what {
				case "services", "":
					printServiceTable(c.ServiceManager.AllStatuses())
				case "projects":
					return printProjects(c)
				case "php":
					return printPHP(c)
				default:
					return fmt.Errorf("unknown list target %q (valid: services, projects, php)", what)
				}
				return nil
			})
		},
	}
	return cmd
}

func newServicesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "services",
		Short: "Alias for `hangar list services`",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withCore(func(c *core.Core) error {
				printServiceTable(c.ServiceManager.AllStatuses())
				return nil
			})
		},
	}
}

func printServiceTable(statuses map[string]services.ServiceStatus) {
	names := make([]string, 0, len(statuses))
	for n := range statuses {
		names = append(names, n)
	}
	// Use the canonical order, then append anything unexpected at the
	// end so a future-added service still shows up.
	canonical := map[string]bool{}
	for _, n := range allServiceNames {
		canonical[n] = true
	}
	sort.SliceStable(names, func(i, j int) bool {
		ci := canonical[names[i]]
		cj := canonical[names[j]]
		if ci != cj {
			return ci
		}
		return names[i] < names[j]
	})

	fmt.Printf("%-12s %-9s %-7s %-10s %s\n", "SERVICE", "STATUS", "PORT", "PID", "UPTIME")
	for _, n := range names {
		st := statuses[n]
		port := "-"
		if st.Port > 0 {
			port = fmt.Sprintf("%d", st.Port)
		}
		pid := "-"
		if st.PID > 0 {
			pid = fmt.Sprintf("%d", st.PID)
		}
		uptime := "-"
		if st.Uptime != "" {
			uptime = st.Uptime
		}
		fmt.Printf("%-12s %-9s %-7s %-10s %s\n",
			n, string(st.Status), port, pid, uptime)
	}
}

func printProjects(c *core.Core) error {
	projects := c.ProjectManager.List()
	if len(projects) == 0 {
		fmt.Println("No projects registered. Create one in the GUI or with `hangar` and add it via Projects.")
		return nil
	}
	fmt.Printf("%-20s %-25s %-10s %-6s %s\n", "NAME", "DOMAIN", "FRAMEWORK", "SSL", "PATH")
	for _, p := range projects {
		ssl := "no"
		if p.SSLEnabled {
			ssl = "yes"
		}
		fmt.Printf("%-20s %-25s %-10s %-6s %s\n",
			truncate(p.Name, 20), truncate(p.Domain, 25),
			truncate(p.Framework, 10), ssl, p.Path)
	}
	return nil
}

func printPHP(c *core.Core) error {
	versions := c.PHPManager.ListInstalled()
	if len(versions) == 0 {
		fmt.Println("No PHP versions installed. Install one from the Packages page in the GUI.")
		return nil
	}
	active := ""
	if cfg, err := c.Config.GetAppConfig(); err == nil {
		active = cfg.ActivePHP
	}
	fmt.Printf("%-10s %-8s %s\n", "VERSION", "ACTIVE", "PATH")
	for _, v := range versions {
		marker := ""
		if v.Version == active {
			marker = "*"
		}
		fmt.Printf("%-10s %-8s %s\n", v.Version, marker, v.Path)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	return s[:n-1] + "…"
}

// ---- daemon ---------------------------------------------------------------

// newDaemonCmd is the headless mode that keeps the MCP server up so AI
// agents can manage Hangar even when the GUI is closed. Implementation
// lives in daemon.go.
func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run Hangar headless (MCP server + tray icon, no main window)",
		Long: "Starts Hangar without opening the GUI window. The MCP server stays\n" +
			"reachable at http://127.0.0.1:3742/sse so AI agents (Claude Code,\n" +
			"Cursor, Windsurf) can manage services even when the GUI is closed.\n" +
			"A system-tray icon appears in the notification area; right-click it\n" +
			"to open the GUI, stop services, or quit.\n\n" +
			"Use `hangar daemon autostart enable` to launch the daemon at every\n" +
			"Windows login. That gives you the same behaviour as Laragon's\n" +
			"\"Auto Start with Windows\" without the visible main window.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunDaemon()
		},
	}
	cmd.AddCommand(newAutostartCmd())
	return cmd
}

// (compile-time check that we wired time + version stamping correctly)
var _ = time.Now
