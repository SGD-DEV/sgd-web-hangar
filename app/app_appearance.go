package app

// app_appearance.go: the Appearance settings - Hangar's name, accent colour
// and logo, and the start page template for new projects.

import (
	"path/filepath"

	"github.com/devour-app/devour/app/appearance"
	"github.com/devour-app/devour/app/projects"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) appearanceStore() *appearance.Store {
	return appearance.NewStore(filepath.Join(a.paths.DataPath(), "appearance"))
}

// initAppearance hooks the start page template into project creation and
// sets the window title. Called once after bootstrap.
func (a *App) initAppearance() {
	store := a.appearanceStore()
	projects.StarterPage = func(name, domain, dir string) string {
		return appearance.RenderStarter(store.Load(), name, domain, dir, "")
	}
	a.applyWindowTitle(store.Load().AppName)
}

func (a *App) applyWindowTitle(name string) {
	if a.ctx != nil && name != "" {
		runtime.WindowSetTitle(a.ctx, name)
	}
}

func (a *App) GetAppearance() appearance.Settings {
	return a.appearanceStore().Load()
}

// GetStarterTexts returns the default heading and text per language.
func (a *App) GetStarterTexts() map[string][2]string {
	return appearance.StarterTexts
}

func (a *App) SaveAppearance(st appearance.Settings) (appearance.Settings, error) {
	store := a.appearanceStore()
	if _, err := store.Save(st); err != nil {
		return st, err
	}
	saved := store.Load()
	a.applyWindowTitle(saved.AppName)
	return saved, nil
}

// PickAppearanceLogo lets the user choose an image file as the logo.
func (a *App) PickAppearanceLogo() (appearance.Settings, error) {
	store := a.appearanceStore()
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Choose a logo",
		Filters: []runtime.FileFilter{{DisplayName: "Images (PNG, JPG, SVG, WebP)", Pattern: "*.png;*.jpg;*.jpeg;*.svg;*.webp"}},
	})
	if err != nil || path == "" {
		return store.Load(), err
	}
	if err := store.SetLogo(path); err != nil {
		return store.Load(), err
	}
	return store.Load(), nil
}

func (a *App) RemoveAppearanceLogo() (appearance.Settings, error) {
	store := a.appearanceStore()
	if err := store.RemoveLogo(); err != nil {
		return store.Load(), err
	}
	return store.Load(), nil
}

// PreviewStarterPage renders the start page template (unsaved settings from
// the form, saved logo) for the live preview.
func (a *App) PreviewStarterPage(st appearance.Settings) string {
	st.Logo = a.appearanceStore().Load().Logo
	return appearance.RenderStarter(st, "mein-projekt", "mein-projekt.test", filepath.Join(a.GetProjectsRoot(), "mein-projekt"), "8.3")
}
