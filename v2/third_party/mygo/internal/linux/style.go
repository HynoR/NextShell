//go:build linux && (amd64 || arm64)

package linux

import (
	"errors"
	"sync"
)

// The app's style sheet (Theme.SetGTKCSS) is a provider of its own, for
// the default screen, at GTK_STYLE_PROVIDER_PRIORITY_USER: above the
// theme's and MyGo's own (titlebar.go), and above the user's gtk.css, which
// has that priority too but was added first. Only apps that set a sheet
// reach this file: the core calls it through platform.StyleBackend.

const priorityUser = 800 // GTK_STYLE_PROVIDER_PRIORITY_USER

var (
	styleOnce                              sync.Once
	gdkScreenGetDefault                    func() ptr
	gtkStyleContextRemoveProviderForScreen func(screen, provider ptr)

	// styleProvider holds the sheet loaded last, 0 for none. Main thread
	// only.
	styleProvider ptr
)

// SetStyleSheet loads css into a new provider and puts it in place of the
// one before, which stays when css does not parse: GTK resets a provider
// before it parses, and keeps the rules before an error.
func (b *Backend) SetStyleSheet(css string) error {
	styleOnce.Do(func() {
		mustBind(libGDK, &gdkScreenGetDefault, "gdk_screen_get_default")
		mustBind(libGTK, &gtkStyleContextRemoveProviderForScreen, "gtk_style_context_remove_provider_for_screen")
	})
	screen := gdkScreenGetDefault()
	var provider ptr
	if css != "" {
		provider = gtkCssProviderNew()
		// With an error to fill, GTK reports the first parse error there
		// rather than as a warning, and returns false.
		var gerr ptr
		if !gtkCssProviderLoadFromData(provider, cs(css), len(css), &gerr) || gerr != 0 {
			gObjectUnref(provider)
			if gerr == 0 {
				return errors.New("GTK did not load the style sheet")
			}
			return gErr(gerr)
		}
		gtkStyleContextAddProviderForScreen(screen, provider, priorityUser)
	}
	if styleProvider != 0 {
		gtkStyleContextRemoveProviderForScreen(screen, styleProvider)
		gObjectUnref(styleProvider)
	}
	styleProvider = provider
	return nil
}
