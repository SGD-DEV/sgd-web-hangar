package projects

import (
	"fmt"

	"github.com/devour-app/devour/app/services"
)

type VhostLinker struct {
	serviceManager *services.Manager
}

func NewVhostLinker(svcMgr *services.Manager) *VhostLinker {
	return &VhostLinker{serviceManager: svcMgr}
}

func (vl *VhostLinker) LinkProject(project Project) error {
	switch project.WebServer {
	case "apache":
		return vl.linkApache(project)
	case "nginx":
		return vl.linkNginx(project)
	default:
		return fmt.Errorf("vhost linker: unknown web server %q", project.WebServer)
	}
}

func (vl *VhostLinker) UnlinkProject(project Project) error {
	switch project.WebServer {
	case "apache":
		return vl.unlinkApache(project)
	case "nginx":
		return vl.unlinkNginx(project)
	default:
		return fmt.Errorf("vhost linker: unknown web server %q", project.WebServer)
	}
}

func (vl *VhostLinker) linkApache(project Project) error {
	svc, err := vl.serviceManager.Get("apache")
	if err != nil {
		return fmt.Errorf("vhost linker: %w", err)
	}

	type apacheVhostCreator interface {
		CreateVhost(vhost interface{}) error
	}

	if creator, ok := svc.(apacheVhostCreator); ok {
		type VirtualHost struct {
			ServerName   string
			DocumentRoot string
			Port         int
			PHPVersion   string
			SSLEnabled   bool
			SSLCertPath  string
			SSLKeyPath   string
		}

		vhost := VirtualHost{
			ServerName:   project.Domain,
			DocumentRoot: project.DocumentRoot,
			Port:         80,
			PHPVersion:   project.PHPVersion,
			SSLEnabled:   project.SSLEnabled,
			SSLCertPath:  project.SSLCertPath,
			SSLKeyPath:   project.SSLKeyPath,
		}
		return creator.CreateVhost(vhost)
	}

	return fmt.Errorf("vhost linker: apache service does not support vhost creation")
}

func (vl *VhostLinker) linkNginx(project Project) error {
	svc, err := vl.serviceManager.Get("nginx")
	if err != nil {
		return fmt.Errorf("vhost linker: %w", err)
	}

	type nginxSiteCreator interface {
		CreateSite(site interface{}) error
	}

	if creator, ok := svc.(nginxSiteCreator); ok {
		type SiteConfig struct {
			ServerName   string
			DocumentRoot string
			Listen       int
			PHPFPMSocket string
			SSLEnabled   bool
			SSLCertPath  string
			SSLKeyPath   string
		}

		site := SiteConfig{
			ServerName:   project.Domain,
			DocumentRoot: project.DocumentRoot,
			Listen:       8080,
			PHPFPMSocket: "127.0.0.1:9000",
			SSLEnabled:   project.SSLEnabled,
			SSLCertPath:  project.SSLCertPath,
			SSLKeyPath:   project.SSLKeyPath,
		}
		return creator.CreateSite(site)
	}

	return fmt.Errorf("vhost linker: nginx service does not support site creation")
}

func (vl *VhostLinker) unlinkApache(project Project) error {
	svc, err := vl.serviceManager.Get("apache")
	if err != nil {
		return fmt.Errorf("vhost linker: %w", err)
	}

	type apacheVhostDeleter interface {
		DeleteVhost(serverName string) error
	}

	if deleter, ok := svc.(apacheVhostDeleter); ok {
		return deleter.DeleteVhost(project.Domain)
	}

	return fmt.Errorf("vhost linker: apache service does not support vhost deletion")
}

func (vl *VhostLinker) unlinkNginx(project Project) error {
	svc, err := vl.serviceManager.Get("nginx")
	if err != nil {
		return fmt.Errorf("vhost linker: %w", err)
	}

	type nginxSiteDeleter interface {
		DeleteSite(serverName string) error
	}

	if deleter, ok := svc.(nginxSiteDeleter); ok {
		return deleter.DeleteSite(project.Domain)
	}

	return fmt.Errorf("vhost linker: nginx service does not support site deletion")
}
