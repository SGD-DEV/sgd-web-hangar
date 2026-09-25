package app

import "testing"

func TestMailpitMailSettings(t *testing.T) {
	in := "[mail function]\r\nSMTP = localhost\r\nsmtp_port = 25\r\n;sendmail_from = me@example.com\r\n"
	want := "[mail function]\r\nSMTP = 127.0.0.1\r\nsmtp_port = 1025\r\nsendmail_from = hangar@localhost\r\n"
	got, changed := mailpitMailSettings([]byte(in))
	if !changed || string(got) != want {
		t.Fatalf("got %q (changed=%v), want %q", got, changed, want)
	}
	if _, changed := mailpitMailSettings(got); changed {
		t.Fatal("second pass changed the file again")
	}

	custom := "SMTP = mail.example.com\nsmtp_port = 587\nsendmail_from = me@site.de\n"
	if out, changed := mailpitMailSettings([]byte(custom)); changed {
		t.Fatalf("custom settings were rewritten: %q", out)
	}
}
