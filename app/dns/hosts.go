package dns

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const hostsFilePath = `C:\Windows\System32\drivers\etc\hosts`

func AddHostEntry(domain, ip string) error {
	if ip == "" {
		ip = "127.0.0.1"
	}

	entries, err := readHostsFile()
	if err != nil {
		return fmt.Errorf("dns hosts: reading: %w", err)
	}

	for _, entry := range entries {
		if entry.domain == domain && entry.ip == ip {
			return nil // already present
		}
	}

	line := fmt.Sprintf("%s\t%s\t# devour", ip, domain)

	// Try direct write first
	f, err := os.OpenFile(hostsFilePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		_, writeErr := f.WriteString("\n" + line + "\n")
		f.Close()
		if writeErr == nil {
			return nil
		}
	}

	// Direct write failed — elevate via PowerShell
	return addHostEntryElevated(line)
}

// AddHostEntries adds multiple host entries at once using a single elevated prompt
func AddHostEntries(domains []string, ip string) error {
	if ip == "" {
		ip = "127.0.0.1"
	}

	entries, err := readHostsFile()
	if err != nil {
		return fmt.Errorf("dns hosts: reading: %w", err)
	}

	existing := make(map[string]bool)
	for _, e := range entries {
		if e.ip != "" && e.domain != "" {
			existing[e.ip+"\t"+e.domain] = true
		}
	}

	var linesToAdd []string
	for _, domain := range domains {
		key := ip + "\t" + domain
		if !existing[key] {
			linesToAdd = append(linesToAdd, fmt.Sprintf("%s\t%s\t# devour", ip, domain))
		}
	}

	if len(linesToAdd) == 0 {
		return nil // all already present
	}

	// Try direct write first
	f, err := os.OpenFile(hostsFilePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		content := "\n" + strings.Join(linesToAdd, "\n") + "\n"
		_, writeErr := f.WriteString(content)
		f.Close()
		if writeErr == nil {
			return nil
		}
	}

	// Direct write failed — elevate via PowerShell (single prompt for all entries)
	content := strings.Join(linesToAdd, "`n")
	psCmd := fmt.Sprintf(`Add-Content -Path '%s' -Value "%s"`, hostsFilePath, content)
	cmd := exec.Command("powershell", "-Command",
		fmt.Sprintf(`Start-Process powershell -Verb RunAs -Wait -ArgumentList '-Command','%s'`,
			strings.ReplaceAll(psCmd, "'", "''")))
	return cmd.Run()
}

func addHostEntryElevated(line string) error {
	psCmd := fmt.Sprintf(`Add-Content -Path '%s' -Value '%s'`, hostsFilePath, line)
	cmd := exec.Command("powershell", "-Command",
		fmt.Sprintf(`Start-Process powershell -Verb RunAs -Wait -ArgumentList '-Command','%s'`,
			strings.ReplaceAll(psCmd, "'", "''")))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("dns hosts: elevated write failed: %s: %w", string(output), err)
	}
	return nil
}

func RemoveHostEntry(domain string) error {
	entries, err := readHostsFile()
	if err != nil {
		return fmt.Errorf("dns hosts: reading: %w", err)
	}

	var newLines []string
	for _, entry := range entries {
		if entry.domain == domain && entry.isDevour {
			continue
		}
		newLines = append(newLines, entry.raw)
	}

	content := strings.Join(newLines, "\n") + "\n"

	// Try direct write first
	if err := os.WriteFile(hostsFilePath, []byte(content), 0644); err == nil {
		return nil
	}

	// Elevate
	psCmd := fmt.Sprintf(`Set-Content -Path '%s' -Value '%s'`,
		hostsFilePath, strings.ReplaceAll(content, "'", "''"))
	cmd := exec.Command("powershell", "-Command",
		fmt.Sprintf(`Start-Process powershell -Verb RunAs -Wait -ArgumentList '-Command','%s'`,
			strings.ReplaceAll(psCmd, "'", "''")))
	return cmd.Run()
}

// HasHostEntry checks if a domain already has an entry in the hosts file
func HasHostEntry(domain string) bool {
	entries, err := readHostsFile()
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.domain == domain {
			return true
		}
	}
	return false
}

type hostEntry struct {
	ip       string
	domain   string
	raw      string
	isDevour bool
}

func readHostsFile() ([]hostEntry, error) {
	f, err := os.Open(hostsFilePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []hostEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		entry := hostEntry{raw: line}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			entries = append(entries, entry)
			continue
		}

		entry.isDevour = strings.Contains(line, "# devour")

		parts := strings.Fields(trimmed)
		if len(parts) >= 2 {
			entry.ip = parts[0]
			entry.domain = parts[1]
		}

		entries = append(entries, entry)
	}

	return entries, scanner.Err()
}
