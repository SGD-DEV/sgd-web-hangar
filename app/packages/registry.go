package packages

type Category string

const (
	CategoryPHP       Category = "php"
	CategoryWebServer Category = "webserver"
	CategoryDatabase  Category = "database"
	CategoryNodeJS    Category = "nodejs"
	CategoryTools     Category = "tools"
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
		{ID: CategoryWebServer, Label: "Web Servers", Description: "Apache and Nginx"},
		{ID: CategoryDatabase, Label: "Databases", Description: "MySQL and PostgreSQL"},
		{ID: CategoryNodeJS, Label: "Node.js", Description: "Node.js runtime versions"},
		{ID: CategoryTools, Label: "Tools", Description: "HeidiSQL, phpMyAdmin, DBeaver, VS Code, PocketBase, Composer"},
		{ID: CategoryGolang, Label: "Golang", Description: "Go programming language"},
	}
}

type CategoryInfo struct {
	ID          Category `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
}
