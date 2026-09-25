//go:build !windows

package winsvc

import (
	"fmt"
	"strings"
)

var errUnsupported = fmt.Errorf("Windows services are only managed on Windows")

func State(string) (string, string)              { return "unknown", "not supported on this OS" }
func Restart(string) error                       { return errUnsupported }
func Start(string) error                         { return errUnsupported }
func Stop(string) error                          { return errUnsupported }
func RunElevatedScript(string) error             { return errUnsupported }
func FindNSSM() string                           { return "" }
func CurrentUserSID() (string, error)            { return "", errUnsupported }
func GrantControlScript(string, string) []string { return nil }
func Q(s string) string                          { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func LastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

type NSSMParams struct {
	Application string
	Parameters  string
	Directory   string
	Environment []string
}

func WriteNSSMParams(string, NSSMParams) error  { return errUnsupported }
func GrantParamsScript(string, string) []string { return nil }
