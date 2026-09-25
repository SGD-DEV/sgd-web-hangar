package projects

type Project struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	Domain       string `json:"domain"`
	PHPVersion   string `json:"php_version"`
	WebServer    string `json:"web_server"`
	SSLEnabled   bool   `json:"ssl_enabled"`
	Framework    string `json:"framework"`
	DocumentRoot string `json:"document_root"`
	SSLCertPath  string `json:"ssl_cert_path"`
	SSLKeyPath   string `json:"ssl_key_path"`
	CreatedAt    string `json:"created_at"`
	// ProxyTarget is set when Framework == "proxy". The web-server vhost
	// generated for this project does NOT serve files - it forwards every
	// request to ProxyTarget (e.g. "http://127.0.0.1:8000" for a Python
	// app you started with `python manage.py runserver`). Empty for any
	// other framework.
	ProxyTarget string `json:"proxy_target,omitempty"`
	// Aliases are extra host names the site answers to, typically the
	// public domains routed here by the Cloudflare Tunnel
	// (e.g. "blog.example.com"). Unlike Domain they are NOT written to the
	// hosts file - they resolve through real DNS.
	Aliases []string `json:"aliases,omitempty"`
	// LocalOnly restricts the site to requests from this machine. Used for
	// the database tools (phpMyAdmin, Adminer) that log in without a
	// password: Apache listens on every interface, so without this anyone
	// on the LAN could reach them by sending the right Host header.
	LocalOnly bool `json:"local_only,omitempty"`
	// Database is the database assigned to the project. Hangar writes it
	// into the project's config (wp-config.php or .env).
	Database *Database `json:"database,omitempty"`
	// Mail is the SMTP server the project sends through. Nil means Mailpit.
	Mail *Mail `json:"mail,omitempty"`
	// App is set for Node/Python/... projects that Hangar runs as a Windows
	// service; the web server proxies to App.Port.
	App *AppService `json:"app,omitempty"`
}

type AppService struct {
	Command      string   `json:"command"`                 // e.g. "npm start", "python app.py"
	Port         int      `json:"port"`                    // passed as PORT, proxied to
	BuildCommand string   `json:"build_command,omitempty"` // run by Deploy, e.g. "npm ci && npm run build"
	Env          []string `json:"env,omitempty"`           // extra KEY=VALUE lines
}

type Database struct {
	Type     string `json:"type"` // "mysql" | "postgresql"
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Name     string `json:"name"`
	User     string `json:"user"`
	Password string `json:"password"`
}

type Mail struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	Password   string `json:"password"`
	Encryption string `json:"encryption"` // "" | "tls" (STARTTLS) | "ssl"
	FromEmail  string `json:"from_email"`
	FromName   string `json:"from_name"`
}

type Framework string

const (
	FrameworkLaravel   Framework = "laravel"
	FrameworkWordPress Framework = "wordpress"
	FrameworkSymfony   Framework = "symfony"
	FrameworkPlainPHP  Framework = "php"
	FrameworkProxy     Framework = "proxy"
	FrameworkUnknown   Framework = "unknown"
)
