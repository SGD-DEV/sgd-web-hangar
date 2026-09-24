package core

import "testing"

func TestPathRewriter(t *testing.T) {
	rw := pathRewriter(`C:\Users\x\AppData\Local\Hangar`, `C:\Hosting\hangar`)
	cases := map[string]string{
		`{"install_path":"C:\\Users\\x\\AppData\\Local\\Hangar\\data\\installed\\mysql\\8.4"}`: `{"install_path":"C:\\Hosting\\hangar\\data\\installed\\mysql\\8.4"}`,
		`datadir=C:\Users\x\AppData\Local\Hangar\data\mysql-data`:                               `datadir=C:\Hosting\hangar\data\mysql-data`,
		`DocumentRoot "C:/Users/x/AppData/Local/Hangar/data/www"`:                              `DocumentRoot "C:/Hosting/hangar/data/www"`,
		`C:\Hosting\www\projects\test`:                                                           `C:\Hosting\www\projects\test`,
	}
	for in, want := range cases {
		if got := string(rw([]byte(in))); got != want {
			t.Errorf("rewrite(%s)\n got %s\nwant %s", in, got, want)
		}
	}
}
