package app

import (
	"strings"
	"testing"
)

func TestHangarINIDefaults(t *testing.T) {
	in := strings.Join([]string{
		"[mail function]",
		"SMTP = localhost",
		"smtp_port = 25",
		";sendmail_from = me@example.com",
		";zend_extension=opcache",
		";realpath_cache_ttl = 120",
		"[opcache]",
		";opcache.enable=1",
		";opcache.enable_cli=0",
		";opcache.memory_consumption=128",
		";opcache.revalidate_freq=2",
		";curl.cainfo =",
		";openssl.cafile=",
		"",
	}, "\r\n")
	got, changed := hangarINIDefaults([]byte(in), `C:\Hosting\data\ssl\hangar-ca-bundle.pem`)
	s := string(got)
	for _, want := range []string{
		"SMTP = 127.0.0.1\r\n",
		"smtp_port = 1025\r\n",
		"sendmail_from = hangar@localhost\r\n",
		"\r\nzend_extension=opcache\r\n",
		"\r\nopcache.enable=1\r\n",
		";opcache.enable_cli=0\r\n", // CLI stays off
		"opcache.memory_consumption=192\r\n",
		"realpath_cache_ttl = 600\r\n",
		`curl.cainfo = "C:\Hosting\data\ssl\hangar-ca-bundle.pem"` + "\r\n",
		`openssl.cafile = "C:\Hosting\data\ssl\hangar-ca-bundle.pem"` + "\r\n",
	} {
		if !changed || !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	if _, again := hangarINIDefaults(got, `C:\x.pem`); again {
		t.Error("second pass changed the file again")
	}

	custom := "SMTP = mail.example.com\nsmtp_port = 587\nopcache.enable=0\ncurl.cainfo = \"D:\\my.pem\"\n"
	if out, changed := hangarINIDefaults([]byte(custom), `C:\x.pem`); changed {
		t.Fatalf("custom settings were rewritten: %q", out)
	}

	// Without a bundle the CA lines stay commented.
	if out, _ := hangarINIDefaults([]byte(";curl.cainfo =\n"), ""); string(out) != ";curl.cainfo =\n" {
		t.Errorf("CA line changed without bundle: %q", out)
	}
}
