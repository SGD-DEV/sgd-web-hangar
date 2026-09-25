package app

import (
	"os"
	"regexp"
)

// PHP's mail() on Windows talks SMTP directly to SMTP:smtp_port. The stock
// php.ini points it at localhost:25 where nothing listens, so mails from
// contact forms were silently lost. Hangar routes them to Mailpit instead.
// Only the untouched stock values are rewritten, so a user's own settings
// survive.
var mailpitINIRewrites = []struct {
	re   *regexp.Regexp
	repl string
}{
	// 127.0.0.1, not localhost: Mailpit listens on IPv4 loopback only and
	// localhost may resolve to ::1 first.
	{regexp.MustCompile(`(?m)^[ \t]*SMTP[ \t]*=[ \t]*localhost[ \t]*(\r?)$`), "SMTP = 127.0.0.1$1"},
	{regexp.MustCompile(`(?m)^[ \t]*smtp_port[ \t]*=[ \t]*25[ \t]*(\r?)$`), "smtp_port = 1025$1"},
	// Without a sender mail() fails on Windows unless the script passes a
	// From header.
	{regexp.MustCompile(`(?m)^;[ \t]*sendmail_from[ \t]*=[ \t]*me@example\.com[ \t]*(\r?)$`), "sendmail_from = hangar@localhost$1"},
}

// mailpitMailSettings returns data with the stock mail settings pointed at
// Mailpit and reports whether anything changed.
func mailpitMailSettings(data []byte) ([]byte, bool) {
	out := data
	for _, r := range mailpitINIRewrites {
		out = r.re.ReplaceAll(out, []byte(r.repl))
	}
	return out, string(out) != string(data)
}

// pointPHPIniAtMailpit applies mailpitMailSettings to an existing php.ini.
func pointPHPIniAtMailpit(iniPath string) error {
	data, err := os.ReadFile(iniPath)
	if err != nil {
		return err
	}
	patched, changed := mailpitMailSettings(data)
	if !changed {
		return nil
	}
	return os.WriteFile(iniPath, patched, 0644)
}
