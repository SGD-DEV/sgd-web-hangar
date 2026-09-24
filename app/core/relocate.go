package core

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/devour-app/devour/app/config"
)

// relocate rewrites absolute paths after the data folder moved (for example
// from %LOCALAPPDATA%\Hangar to C:\Hosting\hangar). Hangar stores absolute
// paths in many places - install locations, project document roots,
// certificate paths, generated my.ini / postgresql.conf / php.ini, the user
// PATH - so moving the folder by hand would leave everything pointing at the
// old location. The previous location is remembered in AppConfig.BasePath;
// configs from before that field existed are recognised by their install
// paths.
func relocate(paths config.Paths, store *config.Store, log Logger) {
	cfg, err := store.GetAppConfig()
	if err != nil {
		return
	}
	newBase := filepath.Clean(paths.BasePath())
	oldBase := cfg.BasePath
	if oldBase == "" {
		oldBase = inferBase(store)
	}

	if oldBase != "" && !strings.EqualFold(filepath.Clean(oldBase), newBase) {
		log(LevelWarn, "data folder moved from %s to %s - rewriting stored paths", oldBase, newBase)
		rewrite := pathRewriter(filepath.Clean(oldBase), newBase)

		if n, err := store.RewriteAll(rewrite); err != nil {
			log(LevelError, "relocate: rewriting config store: %v", err)
		} else {
			log(LevelInfo, "relocate: %d stored values updated", n)
		}
		for _, f := range relocatableFiles(paths) {
			data, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			if nd := rewrite(data); !bytes.Equal(nd, data) {
				if err := os.WriteFile(f, nd, 0644); err != nil {
					log(LevelWarn, "relocate: %s: %v", f, err)
				}
			}
		}
		relocateUserPath(filepath.Clean(oldBase), newBase, log)

		cfg, err = store.GetAppConfig() // reload: RewriteAll changed it
		if err != nil {
			return
		}
	}

	if cfg.BasePath != newBase {
		cfg.BasePath = newBase
		_ = store.SaveAppConfig(cfg)
	}
}

// pathRewriter replaces oldBase with newBase in the spellings Hangar writes:
// plain Windows paths, JSON-escaped (\\) and forward-slash (web server
// configs).
func pathRewriter(oldBase, newBase string) func([]byte) []byte {
	pairs := [][2]string{
		{strings.ReplaceAll(oldBase, `\`, `\\`), strings.ReplaceAll(newBase, `\`, `\\`)},
		{oldBase, newBase},
		{filepath.ToSlash(oldBase), filepath.ToSlash(newBase)},
	}
	return func(v []byte) []byte {
		for _, p := range pairs {
			v = bytes.ReplaceAll(v, []byte(p[0]), []byte(p[1]))
		}
		return v
	}
}

// inferBase finds the old data folder from a service install path such as
// C:\Users\x\AppData\Local\Hangar\data\installed\mysql\8.4.
func inferBase(store *config.Store) string {
	all, err := store.GetAllServiceConfigs()
	if err != nil {
		return ""
	}
	marker := string(filepath.Separator) + filepath.Join("data", "installed") + string(filepath.Separator)
	for _, sc := range all {
		if i := strings.Index(sc.InstallPath, marker); i > 0 {
			return sc.InstallPath[:i]
		}
	}
	return ""
}

// relocatableFiles lists generated text files that embed absolute paths.
func relocatableFiles(paths config.Paths) []string {
	var files []string
	_ = filepath.WalkDir(filepath.Join(paths.DataPath(), "conf"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			switch strings.ToLower(filepath.Ext(p)) {
			case ".ini", ".conf", ".sql", ".cnf":
				files = append(files, p)
			}
		}
		return nil
	})
	globs := []string{
		filepath.Join(paths.DataPath(), "pg-data", "*", "postgresql.conf"),
		filepath.Join(paths.DataPath(), "pg-data", "*", "postgresql.auto.conf"),
		filepath.Join(paths.InstalledPath(), "php", "*", "php.ini"),
		filepath.Join(paths.InstalledPath(), "heidisql", "*", "portable_settings.txt"),
		filepath.Join(paths.InstalledPath(), "heidisql", "*", "*", "portable_settings.txt"),
	}
	for _, g := range globs {
		m, _ := filepath.Glob(g)
		files = append(files, m...)
	}
	return files
}
