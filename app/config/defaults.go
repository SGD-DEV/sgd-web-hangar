package config

// CurrentSchemaVersion is bumped whenever core.Bootstrap needs to migrate an
// existing config (see core.migrateConfig).
const CurrentSchemaVersion = 1

func DefaultAppConfig() AppConfig {
	return AppConfig{
		ProjectsRoot:   "",
		ActivePHP:      "",
		PHPPerProject:  make(map[string]string),
		ActiveVersions: make(map[string]string),
		ApachePort:     80,
		NginxPort:      80,
		MySQLPort:      3306,
		PostgreSQLPort: 5432,
		MCPPort:        3742,
		CLIPort:        3741,
		DNSPort:        53,
		AutoStartAll:   true,
		ApacheEnabled:  true,
		NginxEnabled:   true,
		SSLEnabled:     false,
		Theme:          "dark",

		SchemaVersion:     CurrentSchemaVersion,
		ActiveWebServer:   "apache",
		PHPWorkers:        4,
		TunnelServiceName: "Cloudflared",
	}
}

func DefaultServiceConfig(name string) ServiceConfig {
	defaults := map[string]ServiceConfig{
		"apache": {
			Name:    "apache",
			Port:    80,
			Enabled: true,
		},
		"nginx": {
			Name:    "nginx",
			Port:    80,
			Enabled: true,
		},
		"mysql": {
			Name:    "mysql",
			Port:    3306,
			Enabled: true,
		},
		"postgresql": {
			Name:    "postgresql",
			Port:    5432,
			Enabled: true,
		},
	}

	if cfg, ok := defaults[name]; ok {
		return cfg
	}

	return ServiceConfig{
		Name:    name,
		Enabled: false,
	}
}
