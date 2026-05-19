package heidisql

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type ConnectionParams struct {
	Host     string
	Port     int
	User     string
	Password string
	DBType   string
}

// HeidiSQL NetType values (from TConnectionParameters.FNetType in the
// HeidiSQL source). The previous version of this file mis-labeled 4 as
// "PostgreSQL" - it's actually MS SQL Server (TCP/IP), which is why
// users saw "Provider cannot be found" / MSOLEDBSQL errors when the
// auto-created Postgres session loaded.
const (
	NetTypeMySQLTCP    = 0
	NetTypeMSSQLTCP    = 4 // not what we want for postgres!
	NetTypePostgresTCP = 8
)

// DefaultSession defines a pre-configured session for HeidiSQL
type DefaultSession struct {
	Name    string // Session name shown in HeidiSQL
	Host    string
	Port    int
	User    string
	NetType int    // Use the NetType* constants above
	Library string // optional: which client DLL HeidiSQL should load
}

// EnsureDefaultSessions writes pre-configured sessions to HeidiSQL's
// portable_settings.txt so the user gets a working "Hangar MySQL" and
// "Hangar PostgreSQL" connection without manually entering host/port.
//
// CRITICAL: HeidiSQL's portable_settings.txt is NOT an INI file. It's a
// custom key/value format:
//
//	<key><|||><type><|||><value>
//
// where type is 1 = string, 3 = integer. Sessions live under
// "Servers\<name>\<field>" keys. The previous version wrote
// "[SessionName]\nHost=..." INI-style entries which HeidiSQL silently
// ignored - which is why the user reported having to create the sessions
// manually.
func EnsureDefaultSessions(heidisqlDir string, sessions []DefaultSession) {
	settingsPath := filepath.Join(heidisqlDir, "portable_settings.txt")

	// Read existing content. We skip a session that already has a
	// "Servers\<name>\Host<|||>..." line - don't overwrite a session the
	// user has tuned by hand.
	existing := ""
	if data, err := os.ReadFile(settingsPath); err == nil {
		existing = string(data)
	}

	var lines []string
	for _, s := range sessions {
		marker := "Servers\\" + s.Name + "\\Host<|||>"
		if strings.Contains(existing, marker) {
			continue
		}
		prefix := "Servers\\" + s.Name + "\\"
		lines = append(lines,
			prefix+"Host<|||>1<|||>"+s.Host,
			prefix+"Port<|||>1<|||>"+strconv.Itoa(s.Port),
			prefix+"User<|||>1<|||>"+s.User,
			prefix+"Password<|||>1<|||>",
			prefix+"NetType<|||>3<|||>"+strconv.Itoa(s.NetType),
			prefix+"Compressed<|||>3<|||>0",
			prefix+"LoginPrompt<|||>3<|||>0",
			prefix+"WindowsAuth<|||>3<|||>0",
		)
		if s.Library != "" {
			// Library tells HeidiSQL which DLL to load. PostgreSQL really
			// needs this set explicitly - if absent HeidiSQL may fall
			// back to libmariadb.dll for any session it can't identify,
			// producing the "Provider cannot be found" error the user hit.
			lines = append(lines, prefix+"Library<|||>1<|||>"+s.Library)
		}
	}
	if len(lines) == 0 {
		return
	}

	// HeidiSQL writes CRLF; match that so editors don't show mixed endings.
	f, err := os.OpenFile(settingsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	for _, line := range lines {
		f.WriteString(line + "\r\n")
	}
}

// LaunchSessionManager opens HeidiSQL showing the session manager (no auto-connect)
func LaunchSessionManager(heidisqlDir string) error {
	exePath := filepath.Join(heidisqlDir, "heidisql.exe")
	if _, err := os.Stat(exePath); os.IsNotExist(err) {
		return fmt.Errorf("heidisql: not found at %s", exePath)
	}

	cmd := exec.Command(exePath)
	cmd.Dir = heidisqlDir

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("heidisql: launching: %w", err)
	}

	go cmd.Wait()
	return nil
}

// FindPostgresLibrary is the exported wrapper around findPostgresLibrary.
// app.go uses it when seeding default sessions so the pre-configured
// "Hangar PostgreSQL" session matches whichever libpq DLL the installed
// HeidiSQL actually ships with — defaulting to libpq-15.dll baked the
// version in at compile time and broke on any newer HeidiSQL bundle.
func FindPostgresLibrary(heidisqlDir string) string {
	if lib := findPostgresLibrary(heidisqlDir); lib != "" {
		return lib
	}
	return "libpq-15.dll" // last-resort default
}

// findPostgresLibrary returns the libpq DLL filename HeidiSQL should load
// for a PostgreSQL connection. HeidiSQL ships with versioned DLLs (e.g.
// libpq-15.dll on 12.x) and the bundled name has changed across HeidiSQL
// releases. Without --library= HeidiSQL falls back to libmariadb.dll which
// has no PQconnectdb, producing the "Could not find procedure address for
// PQconnectdb" error we saw.
//
// Returns "" if no candidate exists — caller can omit --library and let
// HeidiSQL try its own default.
func findPostgresLibrary(heidisqlDir string) string {
	candidates := []string{
		"libpq-15.dll",
		"libpq-14.dll",
		"libpq-13.dll",
		"libpq.dll",
	}
	for _, name := range candidates {
		if _, err := os.Stat(filepath.Join(heidisqlDir, name)); err == nil {
			return name
		}
	}
	return ""
}

// Launch opens HeidiSQL and directly connects to the specified database
func Launch(heidisqlDir string, params ConnectionParams) error {
	exePath := filepath.Join(heidisqlDir, "heidisql.exe")
	if _, err := os.Stat(exePath); os.IsNotExist(err) {
		return fmt.Errorf("heidisql: not found at %s", exePath)
	}

	netType := "0" // MySQL TCP/IP
	if params.DBType == "postgresql" {
		netType = "8" // PostgreSQL TCP/IP - 4 is MS SQL Server (the bug
		// the user reported: "Provider cannot be found" + MSOLEDBSQL).
	}

	args := []string{
		fmt.Sprintf("--host=%s", params.Host),
		fmt.Sprintf("--port=%d", params.Port),
		fmt.Sprintf("--user=%s", params.User),
		fmt.Sprintf("--nettype=%s", netType),
	}

	// Tell HeidiSQL which client DLL to load. Without this the postgres
	// session loads libmariadb.dll by default and errors on PQconnectdb.
	if params.DBType == "postgresql" {
		if lib := findPostgresLibrary(heidisqlDir); lib != "" {
			args = append(args, fmt.Sprintf("--library=%s", lib))
		}
	}

	if params.Password != "" {
		args = append(args, fmt.Sprintf("--password=%s", params.Password))
	}

	cmd := exec.Command(exePath, args...)
	cmd.Dir = heidisqlDir

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("heidisql: launching: %w", err)
	}

	go cmd.Wait()
	return nil
}

func IsInstalled(heidisqlDir string) bool {
	exePath := filepath.Join(heidisqlDir, "heidisql.exe")
	_, err := os.Stat(exePath)
	return err == nil
}
