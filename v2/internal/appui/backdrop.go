package appui

import (
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
)

// The picture behind the main window. Without one nothing here changes a pixel: the panels keep their
// opaque colors and the terminal paints the background of its theme, as it always has. With one, the
// window paints it first, dimmed, and the panels, the terminal's ground and the welcome page let part of
// it show through (pane, page and terminalGround).

// backdropState is the picture of Prefs.BackgroundImage once it is read.
type backdropState struct {
	name string     // the file bmp was read from
	bmp  *ui.Bitmap // nil: no picture, or one that cannot be read (name says which, so it is not tried every frame)
}

// chooseBackground picks the picture at src as the background: the platform copies it into the data folder,
// so moving or deleting the original changes nothing, and it is saved in the preferences at once.
func (a *App) chooseBackground(src string) {
	name, bmp, err := a.P.ImportBackground(src)
	if err != nil {
		a.toast(err.Error())
		return
	}
	p := a.Prefs
	p.BackgroundImage = name
	if a.applyPreferences(p) {
		a.settings.draft.BackgroundImage = name
		a.backdrop = backdropState{name: name, bmp: bmp}
	}
}

// clearBackground goes back to the plain window; the copy is deleted with the preference.
func (a *App) clearBackground() {
	p := a.Prefs
	p.BackgroundImage = ""
	if a.applyPreferences(p) {
		a.settings.draft.BackgroundImage = ""
		a.backdrop = backdropState{}
	}
}

// backdropBitmap is the picture to show, read once when the preference names a new one. A copy that is gone
// or cannot be read is told once and the window shows as without a picture.
func (a *App) backdropBitmap() *ui.Bitmap {
	b := &a.backdrop
	name := a.Prefs.BackgroundImage
	if name == b.name {
		return b.bmp
	}
	*b = backdropState{name: name}
	if name == "" {
		return nil
	}
	var err error
	if b.bmp, err = a.P.BackgroundPicture(name); err != nil {
		b.bmp = nil
		a.log("背景图无法读取：" + err.Error())
		a.toast("背景图无法读取，已按没有背景图显示。可以在设置里重新选择")
	}
	return b.bmp
}

// glass is whether the main window shows a picture: the panels are translucent only then.
func (a *App) glass() bool { return a.backdrop.bmp != nil }

// opacity is how much of a panel's color stays over the picture.
func (a *App) opacity() float32 {
	return float32(min(max(a.Prefs.BackgroundOpacity, model.BackgroundOpacityMin), model.BackgroundOpacityMax)) / 100
}

// pane is the face of a bar or a column: the theme's Surface, translucent over a picture.
func (a *App) pane(t *ui.Theme) ui.Color {
	if !a.glass() {
		return t.Surface
	}
	return t.Surface.Alpha(a.opacity())
}

// page is the backing of a page that sits on the window itself, such as the welcome page: none without a
// picture, where the window's own background is under it.
func (a *App) page(t *ui.Theme) ui.Color {
	if !a.glass() {
		return ui.Color{}
	}
	return t.Background.Alpha(a.opacity())
}

// terminalGround is what the transparent terminal sits on. Without a picture it is the background of the
// terminal's theme, as opaque as the terminal itself painted it; over one it is that color dimmed less than
// a panel, so that text stays readable on any picture in both themes.
func (a *App) terminalGround(c *ui.Context) ui.Color {
	light, dark := TerminalThemes(a.Prefs)
	bg := light.Background
	if c.Theme().Dark {
		bg = dark.Background
	}
	if !a.glass() {
		return bg
	}
	o := a.opacity()
	return bg.Alpha(o + (1-o)*.4)
}

// paintBackdrop is the root's drawing under its children: the picture covering the window, under a veil of
// the window's color that grows as the panels get more transparent, so that text keeps its contrast along the
// whole slider.
func (a *App) paintBackdrop(t *ui.Theme, bmp *ui.Bitmap) func(p *ui.Painter, r ui.Rect) {
	veil := t.Background.Alpha(.1 + (1-a.opacity())*.5)
	return func(p *ui.Painter, r ui.Rect) {
		p.Image(bmp, r, ui.Cover)
		p.Fill(r, veil, 0)
	}
}
