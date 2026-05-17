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
