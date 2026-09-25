//go:build windows

package winsvc

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// NSSMParams are the NSSM settings Hangar changes after installation.
type NSSMParams struct {
	Application string
	Parameters  string
	Directory   string
	Environment []string // KEY=VALUE, added to the service's environment
}

func paramsKey(service string) string {
	return `SYSTEM\CurrentControlSet\Services\` + service + `\Parameters`
}

// WriteNSSMParams stores NSSM's settings directly in the registry. The
// install script grants the current user write access to this key, so it
// works unelevated; NSSM reads it on the next service start.
func WriteNSSMParams(service string, p NSSMParams) error {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, paramsKey(service), registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("opening the settings of %s: %w (install the service again)", service, err)
	}
	defer k.Close()
	for name, v := range map[string]string{"Application": p.Application, "AppParameters": p.Parameters, "AppDirectory": p.Directory} {
		if err := k.SetExpandStringValue(name, v); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
	}
	// See InstallAppService: online log rotation can hang the service stop.
	if err := k.SetDWordValue("AppRotateOnline", 0); err != nil {
		return fmt.Errorf("writing AppRotateOnline: %w", err)
	}
	if len(p.Environment) == 0 {
		if err := k.DeleteValue("AppEnvironmentExtra"); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	return k.SetStringsValue("AppEnvironmentExtra", p.Environment)
}

// GrantParamsScript returns PowerShell lines that give the user with sid
// write access to the service's NSSM Parameters key.
func GrantParamsScript(serviceVar, sid string) []string {
	return []string{
		`$key = "HKLM:\SYSTEM\CurrentControlSet\Services\$(` + serviceVar + `)\Parameters"`,
		"$acl = Get-Acl -Path $key", // -LiteralPath fails on registry paths in PowerShell 5.1
		"$rule = New-Object System.Security.AccessControl.RegistryAccessRule((New-Object System.Security.Principal.SecurityIdentifier('" + sid + "')), 'FullControl', 'ContainerInherit', 'None', 'Allow')",
		"$acl.AddAccessRule($rule)",
		"Set-Acl -Path $key -AclObject $acl",
	}
}
