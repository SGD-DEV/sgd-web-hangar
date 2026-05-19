package projects

import (
	"fmt"

	"github.com/devour-app/devour/app/services"
	"github.com/devour-app/devour/app/services/apache"
	"github.com/devour-app/devour/app/services/nginx"
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

	apacheSvc, ok := svc.(*apache.Apache)
	if !ok {
		return fmt.Errorf("vhost linker: apache service has unexpected type")
	}

	return apacheSvc.CreateVhost(apache.VirtualHost{
		ServerName:   project.Domain,
		DocumentRoot: project.DocumentRoot,
		Port:         80,
		PHPVersion:   project.PHPVersion,
		SSLEnabled:   project.SSLEnabled,
		SSLCertPath:  project.SSLCertPath,
		SSLKeyPath:   project.SSLKeyPath,
	})
}

func (vl *VhostLinker) linkNginx(project Project) error {
	svc, err := vl.serviceManager.Get("nginx")
	if err != nil {
		return fmt.Errorf("vhost linker: %w", err)
	}

	nginxSvc, ok := svc.(*nginx.Nginx)
	if !ok {
		return fmt.Errorf("vhost linker: nginx service has unexpected type")
	}

	return nginxSvc.CreateSite(nginx.SiteConfig{
		ServerName:   project.Domain,
		DocumentRoot: project.DocumentRoot,
		Listen:       8080,
		PHPFPMSocket: "127.0.0.1:9000",
		SSLEnabled:   project.SSLEnabled,
		SSLCertPath:  project.SSLCertPath,
		SSLKeyPath:   project.SSLKeyPath,
	})
}

func (vl *VhostLinker) unlinkApache(project Project) error {
	svc, err := vl.serviceManager.Get("apache")
	if err != nil {
		return fmt.Errorf("vhost linker: %w", err)
	}

	apacheSvc, ok := svc.(*apache.Apache)
	if !ok {
		return fmt.Errorf("vhost linker: apache service has unexpected type")
	}
	return apacheSvc.DeleteVhost(project.Domain)
}

func (vl *VhostLinker) unlinkNginx(project Project) error {
	svc, err := vl.serviceManager.Get("nginx")
	if err != nil {
		return fmt.Errorf("vhost linker: %w", err)
	}

	nginxSvc, ok := svc.(*nginx.Nginx)
	if !ok {
		return fmt.Errorf("vhost linker: nginx service has unexpected type")
	}
	return nginxSvc.DeleteSite(project.Domain)
}
