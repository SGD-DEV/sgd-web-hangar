package config

import (
	"fmt"
	"regexp"
	"strings"
)

// DefaultDomainSuffix is used when AppConfig.DomainSuffix is empty. .test is
// reserved for exactly this (RFC 2606) and never resolves on the internet.
const DefaultDomainSuffix = ".test"

var validSuffix = regexp.MustCompile(`^(\.[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?){1,3}$`)

// LocalDomainSuffix is the suffix new projects get, e.g. ".test".
func (c AppConfig) LocalDomainSuffix() string {
	if c.DomainSuffix == "" {
		return DefaultDomainSuffix
	}
	return c.DomainSuffix
}

// NormalizeDomainSuffix lower-cases a suffix from the settings page, adds
// the leading dot and rejects suffixes that would break local sites.
func NormalizeDomainSuffix(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", nil
	}
	if !strings.HasPrefix(s, ".") {
		s = "." + s
	}
	if !validSuffix.MatchString(s) {
		return "", fmt.Errorf("ungültige Domain-Endung %q - z. B. .test oder .intern", s)
	}
	// Browsers force HTTPS for these (HSTS preload), plain http sites
	// would stop working.
	for _, hsts := range []string{".dev", ".app", ".page"} {
		if strings.HasSuffix(s, hsts) {
			return "", fmt.Errorf("%s erzwingt im Browser HTTPS (HSTS) - bitte eine andere Endung wählen, z. B. .test", hsts)
		}
	}
	if s == ".com" || s == ".de" || s == ".net" || s == ".org" {
		return "", fmt.Errorf("%s ist eine echte Top-Level-Domain - bitte eine lokale Endung wie .test verwenden", s)
	}
	return s, nil
}
