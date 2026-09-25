package projects

import (
	"testing"

	"github.com/devour-app/devour/app/config"
)

func TestIsIgnoredFolder(t *testing.T) {
	cfg := config.AppConfig{IgnoredProjectFolders: []string{"Blog.Local"}}
	if !isIgnoredFolder(cfg, "blog.local") {
		t.Error("ignore list must match case-insensitively (Windows paths)")
	}
	if isIgnoredFolder(cfg, "blog") {
		t.Error("only listed folders are ignored")
	}
}
