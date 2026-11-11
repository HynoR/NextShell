package mygo

import (
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// NowPlayingModule shows what the app plays where the system shows media,
// and reports the media keys and the system's media controls as commands.
// Use the NowPlaying singleton.
type NowPlayingModule struct {
	mu      sync.Mutex
	info    NowPlayingInfo
	at      time.Time // when info.Position was
	showing bool
	player  MediaPlayer // the player Control drives, or nil

	onCommand listeners[func(MediaCommand, time.Duration)]
}

// NowPlaying shows what the app plays where the system shows media: Now
// Playing in Control Center and the menu bar on macOS
// (MPNowPlayingInfoCenter), the media controls of the volume flyout and
// the lock screen on Windows (SystemMediaTransportControls), and MPRIS
// clients on Linux, as GNOME's and KDE's media controls and playerctl.
// The media keys of keyboards and headsets, and those controls, send
// commands to the app (OnCommand) while it shows something:
//
//	mygo.NowPlaying.Set(mygo.NowPlayingInfo{
//		Title: "Spring", Artist: "Vivaldi", Album: "The Four Seasons",
//		Artwork: cover, Duration: 3*time.Minute + 20*time.Second,
//		Commands: mygo.MediaPlay | mygo.MediaPause | mygo.MediaNext | mygo.MediaSeek,
//	})
//	mygo.NowPlaying.OnCommand(func(cmd mygo.MediaCommand, pos time.Duration) { … })
//
// An app playing with package audio can let NowPlaying drive the player
// instead (Control).
var NowPlaying = &NowPlayingModule{}

// MediaCommand is a command of the media keys and the system's media
// controls, or a set of them (NowPlayingInfo.Commands).
type MediaCommand uint32

const (
	// MediaPlay and MediaPause are also what the play/pause key sends:
	// MediaPause while the info's State is PlaybackPlaying, else MediaPlay.
	MediaPlay  = MediaCommand(platform.MediaPlay)
	MediaPause = MediaCommand(platform.MediaPause)
	MediaStop  = MediaCommand(platform.MediaStop)
	// MediaNext and MediaPrevious go to the next and the previous track.
	MediaNext     = MediaCommand(platform.MediaNext)
	MediaPrevious = MediaCommand(platform.MediaPrevious)
	// MediaSeek moves to the position OnCommand gets: the system's
	// scrubber, and on Linux MPRIS's Seek and SetPosition.
	MediaSeek = MediaCommand(platform.MediaSeek)
)

// PlaybackState is whether what the app shows plays.
type PlaybackState int

const (
	// PlaybackPlaying is the zero value: an app usually shows what it
	// plays as it starts playing.
	PlaybackPlaying PlaybackState = iota
	PlaybackPaused
	PlaybackStopped
)

// NowPlayingInfo describes what the app plays.
type NowPlayingInfo struct {
	Title, Artist, Album string
	// Artwork is the image of the album or the episode, PNG or JPEG.
	Artwork []byte
	// Duration is how long the track is; 0 is unknown, as for a live
	// stream, which shows no progress.
	Duration time.Duration
	// Position is where playback is as the info is set. The system
	// advances it while the state is PlaybackPlaying, at Rate, so the app
	// sets it again only when playback jumps (SetPlayback).
	Position time.Duration
	// Rate is the speed of playback; 0 is 1.
	Rate  float64
	State PlaybackState
	// Commands are the commands the system's controls offer, as
	// MediaPlay|MediaPause|MediaNext; 0 is MediaPlay|MediaPause|MediaStop.
	Commands MediaCommand
}

// MediaPlayer is a player NowPlaying.Control drives, as an *audio.Player.
type MediaPlayer interface {
	Play()
	Pause()
	Stop()
	Playing() bool
	Seek(time.Duration)
	Position() time.Duration
	Duration() time.Duration
}

// Set shows info where the system shows media, replacing what it showed.
// It may be called before App.Run, and the system shows it once the app
// is ready.
func (n *NowPlayingModule) Set(info NowPlayingInfo) {
	info.Artwork = slices.Clone(info.Artwork)
	n.mu.Lock()
	n.info, n.at, n.showing = info, time.Now(), true
	n.syncLocked()
	n.mu.Unlock()
	n.apply()
}

// SetPlayback changes the state and position of what NowPlaying shows, as
// the app plays, pauses, stops or seeks.
func (n *NowPlayingModule) SetPlayback(state PlaybackState, position time.Duration) {
	n.mu.Lock()
	n.info.State, n.info.Position, n.at = state, position, time.Now()
	showing := n.showing
	n.mu.Unlock()
	if showing {
		n.apply()
	}
}

// Clear removes what NowPlaying shows: the system's controls no longer
// show the app, and the media keys go to other apps.
func (n *NowPlayingModule) Clear() {
	n.mu.Lock()
	showing := n.showing
	n.showing = false
	n.mu.Unlock()
	if showing {
		n.apply()
	}
}

// Info returns what NowPlaying shows, with its Position where playback is
// now, and false if it shows nothing.
func (n *NowPlayingModule) Info() (NowPlayingInfo, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	info := n.info
	info.Artwork = slices.Clone(info.Artwork)
	info.Position = n.positionLocked()
	return info, n.showing
}

// OnCommand is called on the main thread with each command of the media
// keys and the system's controls while NowPlaying shows something:
// position is where MediaSeek moves to, within the track, and for the
// other commands where playback is as the command comes, so that
// SetPlayback(PlaybackPaused, position) keeps the place. The app plays,
// pauses or moves as it asks, and says so with SetPlayback (or Set, for
// another track).
func (n *NowPlayingModule) OnCommand(fn func(cmd MediaCommand, position time.Duration)) (off func()) {
	return n.onCommand.add(fn, false)
}

// Control lets the media keys and the system's controls drive p, as an
// *audio.Player: they play, pause, stop and seek it, and NowPlaying shows
// its state, position and duration, which Set takes from p too. The
// commands Control handles are offered on top of those the info sets,
// MediaSeek when p knows its duration; MediaNext and MediaPrevious still
// go to OnCommand, for the app to change tracks:
//
//	music, err := audio.OpenStream(f)
//	release := mygo.NowPlaying.Control(music)
//	mygo.NowPlaying.Set(mygo.NowPlayingInfo{Title: "Spring", Artist: "Vivaldi"})
//	music.Play()
//	mygo.NowPlaying.Sync()
//
// The system learns what the app does with p itself when it calls Sync;
// a player with an OnEnd(func()) func() method, as *audio.Player, is
// synced at its end. release stops driving p.
func (n *NowPlayingModule) Control(p MediaPlayer) (release func()) {
	off := n.OnCommand(func(cmd MediaCommand, pos time.Duration) {
		switch cmd {
		case MediaPlay:
			p.Play()
		case MediaPause:
			p.Pause()
		case MediaStop:
			p.Stop()
		case MediaSeek:
			p.Seek(pos)
		default:
			return
		}
		n.Sync()
	})
	offEnd := func() {}
	if e, ok := p.(interface{ OnEnd(func()) func() }); ok {
		offEnd = e.OnEnd(n.Sync)
	}
	n.mu.Lock()
	n.player = p
	n.mu.Unlock()
	n.Sync()
	var once sync.Once
	return func() {
		once.Do(func() {
			off()
			offEnd()
			n.mu.Lock()
			if n.player == p {
				n.player = nil
			}
			n.mu.Unlock()
		})
	}
}

// Sync shows the state, position and duration of the player Control
// drives, after the app played, paused, stopped or moved it itself.
func (n *NowPlayingModule) Sync() {
	n.mu.Lock()
	ok := n.syncLocked()
	showing := n.showing
	n.mu.Unlock()
	if ok && showing {
		n.apply()
	}
}

// syncLocked takes the state of the player Control drives, if any. n.mu
// is held.
func (n *NowPlayingModule) syncLocked() bool {
	p := n.player
	if p == nil {
		return false
	}
	pos, dur := p.Position(), p.Duration()
	switch {
	case p.Playing():
		n.info.State = PlaybackPlaying
	case pos == 0 || dur > 0 && pos >= dur:
		n.info.State = PlaybackStopped
	default:
		n.info.State = PlaybackPaused
	}
	n.info.Position, n.info.Duration, n.at = pos, dur, time.Now()
	return true
}

// positionLocked is where playback is now. n.mu is held.
func (n *NowPlayingModule) positionLocked() time.Duration {
	pos := n.info.Position
	if n.info.State == PlaybackPlaying && !n.at.IsZero() {
		pos += time.Duration(float64(time.Since(n.at)) * n.rateLocked())
	}
	if d := n.info.Duration; d > 0 && pos > d {
		pos = d
	}
	return max(pos, 0)
}

func (n *NowPlayingModule) rateLocked() float64 {
	if n.info.Rate > 0 {
		return n.info.Rate
	}
	return 1
}

// commandsLocked are the commands offered. n.mu is held.
func (n *NowPlayingModule) commandsLocked() MediaCommand {
	c := n.info.Commands
	if c == 0 || n.player != nil {
		c |= MediaPlay | MediaPause | MediaStop
	}
	if n.player != nil && n.info.Duration > 0 {
		c |= MediaSeek
	}
	return c
}

// apply has the backend show the latest info, once the app is ready.
// Each apply reads the latest, so they may run in any number.
func (n *NowPlayingModule) apply() {
	App.WhenReady(func() {
		n.mu.Lock()
		var info *platform.NowPlayingInfo
		if n.showing {
			state := [...]string{PlaybackPlaying: "playing", PlaybackPaused: "paused", PlaybackStopped: "stopped"}[min(max(n.info.State, 0), PlaybackStopped)]
			info = &platform.NowPlayingInfo{
				Title: n.info.Title, Artist: n.info.Artist, Album: n.info.Album,
				Artwork:  n.info.Artwork,
				Duration: max(n.info.Duration, 0), Position: n.positionLocked(),
				Rate: n.rateLocked(), State: state,
				Commands: platform.MediaCommand(n.commandsLocked()),
			}
		}
		n.mu.Unlock()
		// The commands and the backend's code are linked into apps that
		// show what they play only.
		mediaCommand = n.command
		linkNowPlaying()
		backend().NowPlaying().Set(info)
	})
}

// mediaCommand is NowPlaying.command once the app showed something.
var mediaCommand func(platform.MediaCommand, time.Duration)

func (appHandler) MediaCommand(cmd platform.MediaCommand, position time.Duration) {
	if mediaCommand != nil {
		mediaCommand(cmd, position)
	}
}

// command reports a command of the backend.
func (n *NowPlayingModule) command(cmd platform.MediaCommand, position time.Duration) {
	n.mu.Lock()
	if !n.showing {
		n.mu.Unlock()
		return
	}
	c := MediaCommand(cmd)
	if cmd == platform.MediaTogglePlayPause {
		c = MediaPlay
		if n.info.State == PlaybackPlaying {
			c = MediaPause
		}
	}
	if c&n.commandsLocked() == 0 {
		n.mu.Unlock()
		return
	}
	if c != MediaSeek {
		position = n.positionLocked()
	}
	if d := n.info.Duration; d > 0 {
		position = min(position, d)
	}
	position = max(position, 0)
	n.mu.Unlock()
	for _, fn := range n.onCommand.snapshot() {
		fn(c, position)
	}
}
