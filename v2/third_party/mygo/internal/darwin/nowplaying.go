//go:build darwin

package darwin

import (
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/internal/platform"
)

func (b *Backend) NowPlaying() platform.NowPlaying { return nowPlaying{b} }

// nowPlaying is MediaPlayer's MPNowPlayingInfoCenter, which Control
// Center and the menu bar show, and MPRemoteCommandCenter, whose commands
// the media keys, headsets and those controls send.
type nowPlaying struct{ b *Backend }

// nowPlayingSet is the code of NowPlaying, which LinkNowPlaying links:
// the backend's methods are linked into every app, and only call it.
var nowPlayingSet func(b *Backend, info *platform.NowPlayingInfo)

// LinkNowPlaying links the code of NowPlaying, when the app first shows
// what it plays.
func LinkNowPlaying() { nowPlayingSet = setNowPlaying }

func (n nowPlaying) Set(info *platform.NowPlayingInfo) {
	if nowPlayingSet != nil {
		nowPlayingSet(n.b, info)
	}
}

// The remote commands, by the command each reports.
var remoteCommands = []struct {
	getter string
	cmd    platform.MediaCommand
}{
	{"playCommand", platform.MediaPlay},
	{"pauseCommand", platform.MediaPause},
	{"togglePlayPauseCommand", platform.MediaTogglePlayPause},
	{"stopCommand", platform.MediaStop},
	{"nextTrackCommand", platform.MediaNext},
	{"previousTrackCommand", platform.MediaPrevious},
	{"changePlaybackPositionCommand", platform.MediaSeek},
}

// MPNowPlayingPlaybackState.
const (
	mpPlaybackPlaying = 1
	mpPlaybackPaused  = 2
	mpPlaybackStopped = 3
)

var (
	libMediaPlayer   uintptr
	mediaPlayerOnce  sync.Once
	remoteHandled    bool // the commands have their handlers
	msgInitArtwork   func(obj id, sel objc.SEL, size NSSize, handler objc.Block) id
	mediaPlayerProps map[string]id
)

// mediaPlayer loads MediaPlayer.framework and its keys, which it exports
// as NSString constants.
func mediaPlayer() bool {
	mediaPlayerOnce.Do(func() {
		lib, err := purego.Dlopen("/System/Library/Frameworks/MediaPlayer.framework/MediaPlayer", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			return
		}
		libMediaPlayer = lib
		purego.RegisterFunc(&msgInitArtwork, msgSendAddr)
		mediaPlayerProps = map[string]id{}
		for _, name := range []string{
			"MPMediaItemPropertyTitle", "MPMediaItemPropertyArtist", "MPMediaItemPropertyAlbumTitle",
			"MPMediaItemPropertyPlaybackDuration", "MPMediaItemPropertyArtwork",
			"MPNowPlayingInfoPropertyElapsedPlaybackTime", "MPNowPlayingInfoPropertyPlaybackRate",
			"MPNowPlayingInfoPropertyDefaultPlaybackRate", "MPNowPlayingInfoPropertyMediaType",
		} {
			if p, err := purego.Dlsym(lib, name); err == nil {
				mediaPlayerProps[name] = **(**id)(unsafe.Pointer(&p))
			}
		}
	})
	return libMediaPlayer != 0
}

func setNowPlaying(b *Backend, info *platform.NowPlayingInfo) {
	if !mediaPlayer() {
		return
	}
	withPool(func() {
		center := send(class("MPNowPlayingInfoCenter"), "defaultCenter")
		commands := send(class("MPRemoteCommandCenter"), "sharedCommandCenter")
		if center == 0 || commands == 0 {
			return
		}
		if !remoteHandled {
			remoteHandled = true
			for _, c := range remoteCommands {
				cmd := c.cmd
				// Blocks share one callback per signature (purego's
				// cache), and the command keeps its copy for good.
				handler := newBlock(func(_ objc.Block, event id) int {
					var pos time.Duration
					if cmd == platform.MediaSeek {
						pos = time.Duration(msgFloat(event, sel("positionTime")) * float64(time.Second))
					}
					b.runOnMain(func() { b.h.MediaCommand(cmd, pos) })
					return 0 // MPRemoteCommandHandlerStatusSuccess
				})
				send(send(commands, c.getter), "addTargetWithHandler:", uintptr(handler))
				handler.Release()
			}
		}
		var enabled platform.MediaCommand
		if info != nil {
			enabled = info.Commands
			if enabled&(platform.MediaPlay|platform.MediaPause) != 0 {
				enabled |= platform.MediaTogglePlayPause
			}
			if info.Duration == 0 {
				enabled &^= platform.MediaSeek
			}
		}
		for _, c := range remoteCommands {
			send(send(commands, c.getter), "setEnabled:", boolArg(enabled&c.cmd != 0))
		}
		if info == nil {
			send(center, "setNowPlayingInfo:", 0)
			send(center, "setPlaybackState:", mpPlaybackStopped)
			return
		}
		d := send(class("NSMutableDictionary"), "dictionary")
		put := func(key string, v id) {
			if k := mediaPlayerProps[key]; k != 0 && v != 0 {
				send(d, "setObject:forKey:", uintptr(v), uintptr(k))
			}
		}
		number := func(v float64) id { return msgFloatID(class("NSNumber"), sel("numberWithDouble:"), v) }
		if info.Title != "" {
			put("MPMediaItemPropertyTitle", nsString(info.Title))
		}
		if info.Artist != "" {
			put("MPMediaItemPropertyArtist", nsString(info.Artist))
		}
		if info.Album != "" {
			put("MPMediaItemPropertyAlbumTitle", nsString(info.Album))
		}
		if info.Duration > 0 {
			put("MPMediaItemPropertyPlaybackDuration", number(info.Duration.Seconds()))
		}
		put("MPNowPlayingInfoPropertyMediaType", nsNumberInt(1)) // MPNowPlayingInfoMediaTypeAudio
		put("MPNowPlayingInfoPropertyElapsedPlaybackTime", number(info.Position.Seconds()))
		put("MPNowPlayingInfoPropertyDefaultPlaybackRate", number(1))
		rate, state := 0.0, mpPlaybackPaused
		switch info.State {
		case "playing":
			rate, state = info.Rate, mpPlaybackPlaying
		case "stopped":
			state = mpPlaybackStopped
		}
		put("MPNowPlayingInfoPropertyPlaybackRate", number(rate))
		if art := artwork(info.Artwork); art != 0 {
			put("MPMediaItemPropertyArtwork", art)
		}
		send(center, "setNowPlayingInfo:", uintptr(d))
		send(center, "setPlaybackState:", uintptr(state))
	})
}

// artwork returns an autoreleased MPMediaItemArtwork of a PNG or JPEG, or
// 0. Its handler decodes the image whenever the system asks, from the
// bytes it holds, so no image outlives the artwork.
func artwork(data []byte) id {
	if len(data) == 0 || msgInitArtwork == nil {
		return 0
	}
	img := send(send(class("NSImage"), "alloc"), "initWithData:", uintptr(nsData(data)))
	if img == 0 {
		return 0
	}
	size := msgSize(img, sel("size"))
	release(img)
	// The handler takes a CGSize, which goes as two doubles.
	handler := newBlock(func(_ objc.Block, w, h float64) id {
		return autorelease(send(send(class("NSImage"), "alloc"), "initWithData:", uintptr(nsData(data))))
	})
	defer handler.Release()
	art := msgInitArtwork(send(class("MPMediaItemArtwork"), "alloc"), sel("initWithBoundsSize:requestHandler:"), size, handler)
	return autorelease(art)
}
