package packages

type Category string

const (
	CategoryPHP       Category = "php"
	CategoryWebServer Category = "webserver"
	CategoryDatabase  Category = "database"
	CategorySearch    Category = "search"
	CategoryNodeJS    Category = "nodejs"
	CategoryPython    Category = "python"
	CategoryTools     Category = "tools"
	CategoryCloud     Category = "cloud"
	CategoryGolang    Category = "golang"
)

type PackageEntry struct {
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	Version  string   `json:"version"`
	URL      string   `json:"url"`
	Category Category `json:"category"`
	SubDir   string   `json:"sub_dir"`
}

func AllPackages() []PackageEntry {
	return []PackageEntry{
		// PHP
		// PHP 8.5 is the newest stable. Bundle is "8.3 = recommended" because
		// the broadest ecosystem (Laravel 11, WordPress, most prod hosts) is
		// still pinned at 8.3 LTS-style support windows.
		{Name: "php", Label: "PHP 8.5 (latest)", Version: "8.5", URL: "https://windows.php.net/downloads/releases/archives/php-8.5.4-nts-Win32-vs17-x64.zip", Category: CategoryPHP, SubDir: "php/8.5"},
		{Name: "php", Label: "PHP 8.4", Version: "8.4", URL: "https://windows.php.net/downloads/releases/archives/php-8.4.12-nts-Win32-vs17-x64.zip", Category: CategoryPHP, SubDir: "php/8.4"},
		{Name: "php", Label: "PHP 8.3 (recommended)", Version: "8.3", URL: "https://windows.php.net/downloads/releases/archives/php-8.3.25-nts-Win32-vs16-x64.zip", Category: CategoryPHP, SubDir: "php/8.3"},
		{Name: "php", Label: "PHP 8.2", Version: "8.2", URL: "https://windows.php.net/downloads/releases/archives/php-8.2.28-nts-Win32-vs16-x64.zip", Category: CategoryPHP, SubDir: "php/8.2"},
		{Name: "php", Label: "PHP 8.1", Version: "8.1", URL: "https://windows.php.net/downloads/releases/archives/php-8.1.32-nts-Win32-vs16-x64.zip", Category: CategoryPHP, SubDir: "php/8.1"},
		{Name: "php", Label: "PHP 7.3", Version: "7.3", URL: "https://windows.php.net/downloads/releases/archives/php-7.3.33-nts-Win32-VC15-x64.zip", Category: CategoryPHP, SubDir: "php/7.3"},

		// Web Servers
		{Name: "apache", Label: "Apache 2.4.65", Version: "2.4.65", URL: "https://www.apachelounge.com/download/VS17/binaries/httpd-2.4.65-250724-Win64-VS17.zip", Category: CategoryWebServer, SubDir: "apache/2.4.65"},
		{Name: "apache", Label: "Apache 2.4.57", Version: "2.4.57", URL: "https://www.apachelounge.com/download/VS16/binaries/httpd-2.4.57-win64-VS16.zip", Category: CategoryWebServer, SubDir: "apache/2.4.57"},
		{Name: "nginx", Label: "Nginx 1.29.1", Version: "1.29.1", URL: "https://nginx.org/download/nginx-1.29.1.zip", Category: CategoryWebServer, SubDir: "nginx/1.29.1"},

		// MySQL
		{Name: "mysql", Label: "MySQL 9.4", Version: "9.4", URL: "https://dev.mysql.com/get/Downloads/MySQL-9.4/mysql-9.4.0-winx64.zip", Category: CategoryDatabase, SubDir: "mysql/9.4"},
		{Name: "mysql", Label: "MySQL 8.4", Version: "8.4", URL: "https://dev.mysql.com/get/Downloads/MySQL-8.4/mysql-8.4.6-winx64.zip", Category: CategoryDatabase, SubDir: "mysql/8.4"},
		{Name: "mysql", Label: "MySQL 8.0", Version: "8.0", URL: "https://dev.mysql.com/get/Downloads/MySQL-8.0/mysql-8.0.40-winx64.zip", Category: CategoryDatabase, SubDir: "mysql/8.0"},
		{Name: "mysql", Label: "MySQL 5.7", Version: "5.7", URL: "https://dev.mysql.com/get/Downloads/MySQL-5.7/mysql-5.7.39-winx64.zip", Category: CategoryDatabase, SubDir: "mysql/5.7"},

		// PostgreSQL — use get.enterprisedb.com full binaries (includes share/postgres.bki needed for initdb)
		{Name: "postgresql", Label: "PostgreSQL 18.3", Version: "18.3", URL: "https://get.enterprisedb.com/postgresql/postgresql-18.3-1-windows-x64-binaries.zip", Category: CategoryDatabase, SubDir: "postgresql/18.3"},
		{Name: "postgresql", Label: "PostgreSQL 17.9", Version: "17.9", URL: "https://get.enterprisedb.com/postgresql/postgresql-17.9-1-windows-x64-binaries.zip", Category: CategoryDatabase, SubDir: "postgresql/17.9"},
		{Name: "postgresql", Label: "PostgreSQL 16.13", Version: "16.13", URL: "https://get.enterprisedb.com/postgresql/postgresql-16.13-1-windows-x64-binaries.zip", Category: CategoryDatabase, SubDir: "postgresql/16.13"},
		{Name: "postgresql", Label: "PostgreSQL 15.17", Version: "15.17", URL: "https://get.enterprisedb.com/postgresql/postgresql-15.17-1-windows-x64-binaries.zip", Category: CategoryDatabase, SubDir: "postgresql/15.17"},

		// Node.js
		{Name: "node", Label: "Node.js 24.9", Version: "24.9", URL: "https://nodejs.org/dist/v24.9.0/node-v24.9.0-win-x64.zip", Category: CategoryNodeJS, SubDir: "node/24.9"},
		{Name: "node", Label: "Node.js 23.11", Version: "23.11", URL: "https://nodejs.org/dist/v23.11.0/node-v23.11.0-win-x64.zip", Category: CategoryNodeJS, SubDir: "node/23.11"},
		{Name: "node", Label: "Node.js 22.14", Version: "22.14", URL: "https://nodejs.org/dist/v22.14.0/node-v22.14.0-win-x64.zip", Category: CategoryNodeJS, SubDir: "node/22.14"},

		// Tools
		{Name: "phpmyadmin", Label: "phpMyAdmin 5.2.2", Version: "5.2.2", URL: "https://files.phpmyadmin.net/phpMyAdmin/5.2.2/phpMyAdmin-5.2.2-english.zip", Category: CategoryTools, SubDir: "phpmyadmin/5.2.2"},
		{Name: "phpmyadmin", Label: "phpMyAdmin 6.0-snapshot", Version: "6.0-snapshot", URL: "https://files.phpmyadmin.net/snapshots/phpMyAdmin-6.0+snapshot-english.tar.xz", Category: CategoryTools, SubDir: "phpmyadmin/6.0-snapshot"},
		{Name: "dbeaver", Label: "DBeaver CE", Version: "latest", URL: "https://dbeaver.io/files/dbeaver-ce-latest-win32.win32.x86_64.zip", Category: CategoryTools, SubDir: "dbeaver/latest"},
		{Name: "vscode", Label: "VS Code", Version: "latest", URL: "https://go.microsoft.com/fwlink/?Linkid=850641", Category: CategoryTools, SubDir: "vscode/latest"},
		// HeidiSQL is hosted on GitHub Releases now - heidisql.com only keeps
		// the older 12.8 zip directly. Verified the 12.17 GH URL responds
		// 302 -> Azure blob, ~29MB. Bumping the bootstrap bundle to match
		// so first-install ships the current version.
		{Name: "heidisql", Label: "HeidiSQL 12.17", Version: "12.17", URL: "https://github.com/HeidiSQL/HeidiSQL/releases/download/12.17/HeidiSQL_12.17_64_Portable.zip", Category: CategoryTools, SubDir: "heidisql/12.17"},
		{Name: "pocketbase", Label: "PocketBase", Version: "0.25.9", URL: "https://github.com/pocketbase/pocketbase/releases/download/v0.25.9/pocketbase_0.25.9_windows_amd64.zip", Category: CategoryTools, SubDir: "pocketbase/0.25.9"},
		// Composer is a single composer.phar, not an archive. SubDir is intentionally
		// flat ("composer", no version) to match the layout app.go::findComposer expects.
		{Name: "composer", Label: "Composer 2.8.4", Version: "2.8.4", URL: "https://getcomposer.org/download/2.8.4/composer.phar", Category: CategoryTools, SubDir: "composer"},

		// Mail
		{Name: "mailpit", Label: "Mailpit 1.24", Version: "1.24", URL: "https://github.com/axllent/mailpit/releases/download/v1.24.2/mailpit-windows-amd64.zip", Category: CategoryTools, SubDir: "mailpit/1.24"},

		// Golang
		{Name: "go", Label: "Go 1.24", Version: "1.24", URL: "https://go.dev/dl/go1.24.1.windows-amd64.zip", Category: CategoryGolang, SubDir: "go/1.24"},
		{Name: "go", Label: "Go 1.23", Version: "1.23", URL: "https://go.dev/dl/go1.23.4.windows-amd64.zip", Category: CategoryGolang, SubDir: "go/1.23"},

		// --- v1.2 ADDITIONS (research workflow verified 2026-06-03) ---

		// Node version managers - nvm-windows is the de-facto standard;
		// fnm is the modern Rust-based alternative. We ship both and let
		// the user pick; both extract a single nvm.exe / fnm.exe.
		{Name: "nvm-windows", Label: "nvm-windows 1.2.2", Version: "1.2.2", URL: "https://github.com/coreybutler/nvm-windows/releases/download/1.2.2/nvm-noinstall.zip", Category: CategoryNodeJS, SubDir: "nvm-windows/1.2.2"},
		{Name: "fnm", Label: "fnm 1.39.0", Version: "1.39.0", URL: "https://github.com/Schniz/fnm/releases/download/v1.39.0/fnm-windows.zip", Category: CategoryNodeJS, SubDir: "fnm/1.39.0"},

		// Bun - modern JS runtime, Windows-native since 2024. Distinct
		// from Node.js (it's a separate binary), so we surface it as a
		// sibling under the Node.js category rather than overloading
		// node/<version>.
		{Name: "bun", Label: "Bun 1.3.14", Version: "1.3.14", URL: "https://github.com/oven-sh/bun/releases/download/bun-v1.3.14/bun-windows-x64.zip", Category: CategoryNodeJS, SubDir: "bun/1.3.14"},

		// Python toolchain - uv is Astral's single-binary Rust tool that
		// replaces pip + virtualenv + pyenv + poetry. The whole Python
		// category exists for this one entry today; future entries
		// (pyenv-win, the Python runtime itself) will join later.
		{Name: "uv", Label: "Astral uv 0.11.18", Version: "0.11.18", URL: "https://github.com/astral-sh/uv/releases/download/0.11.18/uv-x86_64-pc-windows-msvc.zip", Category: CategoryPython, SubDir: "uv/0.11.18"},

		// Search engines - Meilisearch ships as a single .exe; no zip,
		// just rename to the standard mainExe. Typesense is intentionally
		// not bundled: upstream ships Linux/Docker only on Windows.
		{Name: "meilisearch", Label: "Meilisearch 1.45.2", Version: "1.45.2", URL: "https://github.com/meilisearch/meilisearch/releases/download/v1.45.2/meilisearch-windows-amd64.exe", Category: CategorySearch, SubDir: "meilisearch/1.45.2"},

		// MongoDB Community - full Windows zip with bin/mongod.exe and
		// supporting tools. ~140 MB. Service implementation needed - see
		// app/services/mongodb.
		{Name: "mongodb", Label: "MongoDB 8.0", Version: "8.0.23", URL: "https://fastdl.mongodb.org/windows/mongodb-windows-x86_64-8.0.23.zip", Category: CategoryDatabase, SubDir: "mongodb/8.0.23"},

		// Caddy - modern web server with auto-HTTPS. Ports 80/443
		// collide with Apache/Nginx, so we register it but the user
		// must explicitly enable it (Servers page will refuse to start
		// Caddy if Apache or Nginx is already running on 80).
		{Name: "caddy", Label: "Caddy 2.11.4", Version: "2.11.4", URL: "https://github.com/caddyserver/caddy/releases/download/v2.11.4/caddy_2.11.4_windows_amd64.zip", Category: CategoryWebServer, SubDir: "caddy/2.11.4"},

		// Adminer - single-PHP-file DB admin tool. Even lighter than
		// phpMyAdmin. Lives under tools because it's a web app served
		// via Apache/Nginx (same model as phpmyadmin).
		{Name: "adminer", Label: "Adminer 5.4.2", Version: "5.4.2", URL: "https://github.com/vrana/adminer/releases/download/v5.4.2/adminer-5.4.2.php", Category: CategoryTools, SubDir: "adminer/5.4.2"},

		// PHP framework CLIs - all single executables / phars
		{Name: "wp-cli", Label: "WP-CLI 2.12.0", Version: "2.12.0", URL: "https://github.com/wp-cli/wp-cli/releases/download/v2.12.0/wp-cli-2.12.0.phar", Category: CategoryTools, SubDir: "wp-cli/2.12.0"},
		{Name: "symfony-cli", Label: "Symfony CLI 5.17.1", Version: "5.17.1", URL: "https://github.com/symfony-cli/symfony-cli/releases/download/v5.17.1/symfony-cli_windows_amd64.zip", Category: CategoryTools, SubDir: "symfony-cli/5.17.1"},

		// GitHub CLI - widely used by indie devs. Lives in tools, not
		// cloud, because it's not a deploy CLI.
		{Name: "gh", Label: "GitHub CLI 2.93.0", Version: "2.93.0", URL: "https://github.com/cli/cli/releases/download/v2.93.0/gh_2.93.0_windows_amd64.zip", Category: CategoryTools, SubDir: "gh/2.93.0"},

		// Cloud deploy CLIs - these all install single Go-built
		// binaries; no NPM / no shared runtime. AWS / Vercel / Netlify /
		// Wrangler are deferred until we have an install-via-npm and
		// install-via-MSI flow.
		{Name: "supabase", Label: "Supabase CLI 2.104.0", Version: "2.104.0", URL: "https://github.com/supabase/cli/releases/download/v2.104.0/supabase_2.104.0_windows_amd64.tar.gz", Category: CategoryCloud, SubDir: "supabase/2.104.0"},
		{Name: "flyctl", Label: "Fly.io CLI 0.4.57", Version: "0.4.57", URL: "https://github.com/superfly/flyctl/releases/download/v0.4.57/flyctl_0.4.57_Windows_x86_64.zip", Category: CategoryCloud, SubDir: "flyctl/0.4.57"},
		{Name: "cloudflared", Label: "Cloudflared 2026.5.2", Version: "2026.5.2", URL: "https://github.com/cloudflare/cloudflared/releases/download/2026.5.2/cloudflared-windows-amd64.exe", Category: CategoryCloud, SubDir: "cloudflared/2026.5.2"},
	}
}

func PackagesByCategory(cat Category) []PackageEntry {
	var result []PackageEntry
	for _, p := range AllPackages() {
		if p.Category == cat {
			result = append(result, p)
		}
	}
	return result
}

func FindPackage(name, version string) *PackageEntry {
	for _, p := range AllPackages() {
		if p.Name == name && p.Version == version {
			return &p
		}
	}
	return nil
}

func Categories() []CategoryInfo {
	return []CategoryInfo{
		{ID: CategoryPHP, Label: "PHP", Description: "PHP runtime versions"},
		{ID: CategoryWebServer, Label: "Web Servers", Description: "Apache, Nginx, and Caddy"},
		{ID: CategoryDatabase, Label: "Databases", Description: "MySQL, PostgreSQL, and MongoDB"},
		{ID: CategorySearch, Label: "Search", Description: "Search engines (Meilisearch)"},
		{ID: CategoryNodeJS, Label: "Node.js", Description: "Node.js, Bun, and version managers (nvm, fnm)"},
		{ID: CategoryPython, Label: "Python", Description: "Python toolchain (uv)"},
		{ID: CategoryTools, Label: "Tools", Description: "DB admin, editors, CLIs (phpMyAdmin, Adminer, DBeaver, VS Code, gh, wp-cli, symfony, Composer)"},
		{ID: CategoryCloud, Label: "Cloud", Description: "Deploy CLIs (Cloudflared, Supabase, Fly.io)"},
		{ID: CategoryGolang, Label: "Golang", Description: "Go programming language"},
	}
}

type CategoryInfo struct {
	ID          Category `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
}
