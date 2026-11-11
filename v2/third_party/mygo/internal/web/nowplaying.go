//go:build js

package web

import (
	"bytes"
	"syscall/js"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

func (b *Backend) NowPlaying() platform.NowPlaying { return nowPlaying{b} }

// nowPlaying is the page's Media Session (navigator.mediaSession), which
// browsers show in their media controls and answer the media keys with.
type nowPlaying struct{ b *Backend }

var (
	// mediaActions are the session's actions, by the command each sends,
	// and their handlers, made once.
	mediaActions = []struct {
		name string
		cmd  platform.MediaCommand
	}{
		{"play", platform.MediaPlay},
		{"pause", platform.MediaPause},
		{"stop", platform.MediaStop},
		{"nexttrack", platform.MediaNext},
		{"previoustrack", platform.MediaPrevious},
		{"seekto", platform.MediaSeek},
	}
	mediaHandlers []js.Func
	artworkURL    string // the object URL of the artwork shown
)

func (n nowPlaying) Set(info *platform.NowPlayingInfo) {
	session := js.Global().Get("navigator").Get("mediaSession")
	if session.IsUndefined() {
		return
	}
	if mediaHandlers == nil {
		for _, a := range mediaActions {
			cmd := a.cmd
			mediaHandlers = append(mediaHandlers, n.b.funcOf(func(_ js.Value, args []js.Value) any {
				var pos time.Duration
				if cmd == platform.MediaSeek && len(args) > 0 {
					pos = time.Duration(args[0].Get("seekTime").Float() * float64(time.Second))
				}
				n.b.sync(func() { n.b.h.MediaCommand(cmd, pos) })
				return nil
			}))
		}
	}
	if artworkURL != "" {
		js.Global().Get("URL").Call("revokeObjectURL", artworkURL)
		artworkURL = ""
	}
	if info == nil {
		session.Set("metadata", js.Null())
		session.Set("playbackState", "none")
		for _, a := range mediaActions {
			setActionHandler(session, a.name, js.Null())
		}
		return
	}
	meta := map[string]any{"title": info.Title, "artist": info.Artist, "album": info.Album}
	if len(info.Artwork) > 0 {
		typ := "image/jpeg"
		if bytes.HasPrefix(info.Artwork, []byte("\x89PNG")) {
			typ = "image/png"
		}
		data := js.Global().Get("Uint8Array").New(len(info.Artwork))
		js.CopyBytesToJS(data, info.Artwork)
		blob := js.Global().Get("Blob").New([]any{data}, map[string]any{"type": typ})
		artworkURL = js.Global().Get("URL").Call("createObjectURL", blob).String()
		meta["artwork"] = []any{map[string]any{"src": artworkURL, "type": typ}}
	}
	session.Set("metadata", js.Global().Get("MediaMetadata").New(meta))
	state := info.State
	if state == "stopped" {
		state = "none"
	}
	session.Set("playbackState", state)
	for i, a := range mediaActions {
		h := js.Null()
		if info.Commands&a.cmd != 0 {
			h = mediaHandlers[i].Value
		}
		setActionHandler(session, a.name, h)
	}
	if session.Get("setPositionState").Type() == js.TypeFunction {
		if info.Duration > 0 {
			call(session, "setPositionState", map[string]any{
				"duration":     info.Duration.Seconds(),
				"position":     min(info.Position, info.Duration).Seconds(),
				"playbackRate": info.Rate,
			})
		} else {
			call(session, "setPositionState")
		}
	}
}

// setActionHandler sets the handler of an action, which browsers that do
// not know the action reject.
func setActionHandler(session js.Value, action string, h js.Value) {
	call(session, "setActionHandler", action, h)
}

// call calls a method, ignoring what it throws.
func call(v js.Value, method string, args ...any) {
	defer func() { _ = recover() }()
	v.Call(method, args...)
}
