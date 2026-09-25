package appearance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	s := NewStore(t.TempDir())
	if got := s.Load(); got.AppName != "Hangar" || got.Starter.Lang != "de" {
		t.Fatalf("defaults: %+v", got)
	}
	st := Defaults()
	st.AppName = "RS Hosting"
	st.Accent = "#3b82f6"
	if _, err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	if got := s.Load(); got.AppName != "RS Hosting" || got.Accent != "#3b82f6" {
		t.Fatalf("after save: %+v", got)
	}
	st.Accent = "blue"
	if _, err := s.Save(st); err == nil {
		t.Error("invalid colour accepted")
	}
}

func TestLogo(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, "appearance"))
	png := filepath.Join(dir, "a.png")
	os.WriteFile(png, []byte("\x89PNG fake"), 0644)
	if err := s.SetLogo(png); err != nil {
		t.Fatal(err)
	}
	if got := s.Load().Logo; !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("logo = %q", got)
	}
	svg := filepath.Join(dir, "b.svg")
	os.WriteFile(svg, []byte("<svg/>"), 0644)
	s.SetLogo(svg)
	if got := s.Load().Logo; !strings.HasPrefix(got, "data:image/svg+xml;base64,") {
		t.Fatalf("replaced logo = %q", got)
	}
	if err := s.SetLogo(filepath.Join(dir, "x.exe")); err == nil {
		t.Error("exe accepted as logo")
	}
	s.RemoveLogo()
	if s.Load().Logo != "" {
		t.Error("logo not removed")
	}
}

func TestRenderStarter(t *testing.T) {
	st := Defaults()
	st.Starter.Heading = "{name} <b>live</b>"
	page := RenderStarter(st, "shop", "shop.test", `C:\www\shop`, "")
	for _, want := range []string{
		`<html lang="de">`,
		"<h1>shop &lt;b&gt;live&lt;/b&gt;</h1>",
		"<code><?= PHP_VERSION ?></code>",
		`<code>C:\www\shop</code>`,
		"background: #b5f23d",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %q in\n%s", want, page)
		}
	}
	st.Starter.ShowPHP = false
	st.Starter.Background = "#ffffff"
	page = RenderStarter(st, "shop", "shop.test", `C:\www\shop`, "8.3")
	if strings.Contains(page, "PHP") && strings.Contains(page, "8.3") {
		t.Errorf("PHP sentence not removed:\n%s", page)
	}
	if !strings.Contains(page, "color: #16181a") {
		t.Error("dark text expected on white background")
	}
}
