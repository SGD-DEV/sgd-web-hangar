package config

import "testing"

func TestNormalizeDomainSuffix(t *testing.T) {
	for in, want := range map[string]string{"": "", "test": ".test", " .LOCAL ": ".local", ".intern.lan": ".intern.lan"} {
		if got, err := NormalizeDomainSuffix(in); err != nil || got != want {
			t.Errorf("%q -> %q, %v (want %q)", in, got, err, want)
		}
	}
	for _, bad := range []string{".dev", ".app", ".de", "a b", ".-x", "..test"} {
		if _, err := NormalizeDomainSuffix(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if got := (AppConfig{}).LocalDomainSuffix(); got != ".test" {
		t.Errorf("default = %q", got)
	}
}
