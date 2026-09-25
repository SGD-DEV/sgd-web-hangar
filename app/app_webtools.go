package app

// app_webtools.go: the "Web-Oberflächen" links in the sidebar - browser UIs
// of services that are easy to forget the address of.

import (
	"github.com/devour-app/devour/app/services"
	"github.com/devour-app/devour/app/services/meilisearch"
)

// WebTool is one link in the sidebar.
type WebTool struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	URL       string `json:"url"`
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	// Key is shown for copying when the UI asks for one (Meilisearch).
	Key string `json:"key,omitempty"`
}

// GetWebTools lists the service UIs. phpMyAdmin and Adminer are opened
// through OpenPhpMyAdmin/OpenAdminer, which register their sites first.
func (a *App) GetWebTools() []WebTool {
	tools := []WebTool{
		{ID: "mailpit", Label: "Mailpit", URL: "http://127.0.0.1:8025"},
		{ID: "meilisearch", Label: "Meilisearch", URL: "http://127.0.0.1:7700", Key: meilisearch.MasterKey()},
	}
	for i := range tools {
		tools[i].Installed = a.isInstalled(tools[i].ID)
		if a.serviceManager != nil {
			if st, err := a.serviceManager.Status(tools[i].ID); err == nil {
				tools[i].Running = st.Status == services.StatusRunning
			}
		}
	}
	return tools
}
