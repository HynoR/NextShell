//go:build windows && (amd64 || arm64)

package windows

import (
	"slices"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

func (b *Backend) NowPlaying() platform.NowPlaying { return nowPlaying{b} }

// nowPlaying is the SystemMediaTransportControls of the application
// window, through ISystemMediaTransportControlsInterop as desktop apps
// get them: the media controls of the volume flyout and the lock screen,
// which the media keys drive.
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

// The Windows Runtime's functions and the interfaces of the controls,
// made on first use: what package variables hold is linked into every app.
var (
	winrtOnce sync.Once
	winrtFns  *winrtAPI
)

func winrt() *winrtAPI {
	winrtOnce.Do(func() {
		winrtFns = newWinRT()
	})
	return winrtFns
}

func newWinRT() *winrtAPI {
	combase := systemDLL("combase.dll")
	return &winrtAPI{
		getActivationFactory: combase.NewProc("RoGetActivationFactory"),
		activateInstance:     combase.NewProc("RoActivateInstance"),
		createString:         combase.NewProc("WindowsCreateString"),
		deleteString:         combase.NewProc("WindowsDeleteString"),
		streamOverStream:     shcore.NewProc("CreateRandomAccessStreamOverStream"),

		iidInterop:      guid("ddb0472d-c911-4a1f-86d9-dc3d71a95f5a"),
		iidControls:     guid("99fa3ff4-1742-42a6-902e-087d41f965ec"),
		iidControls2:    guid("ea98d2f6-7f3c-4af2-a586-72889808efb1"),
		iidMusic2:       guid("00368462-97d3-44b9-b00f-008afcefaf18"),
		iidTimeline:     guid("5125316a-c3a2-475b-8507-93534dc88f15"),
		iidStream:       guid("905a0fe1-bc53-11df-8c49-001e4fc686da"),
		iidStreamRefNew: guid("857309dc-3fbf-4e7d-986f-ef3b1a07a964"),
	}
}

type winrtAPI struct {
	getActivationFactory, activateInstance, createString, deleteString, streamOverStream *syscall.LazyProc

	// ISystemMediaTransportControlsInterop, ISystemMediaTransportControls
	// and its second version, IMusicDisplayProperties2,
	// ISystemMediaTransportControlsTimelineProperties,
	// IRandomAccessStream and IRandomAccessStreamReferenceStatics.
	iidInterop, iidControls, iidControls2, iidMusic2, iidTimeline, iidStream, iidStreamRefNew GUID
}

// Vtable indices (windows.media.h); IInspectable takes the first six.
const (
	smtcInteropGetForWindow = 6

	smtcPutPlaybackStatus    = 7
	smtcGetDisplayUpdater    = 8
	smtcPutIsEnabled         = 11
	smtcPutIsPlayEnabled     = 13
	smtcPutIsStopEnabled     = 15
	smtcPutIsPauseEnabled    = 17
	smtcPutIsPreviousEnabled = 25
	smtcPutIsNextEnabled     = 27
	smtcAddButtonPressed     = 32

	smtc2PutPlaybackRate                    = 11
	smtc2UpdateTimelineProperties           = 12
	smtc2AddPlaybackPositionChangeRequested = 13

	updaterPutType               = 7
	updaterPutThumbnail          = 11
	updaterGetMusicProperties    = 12
	updaterClearAll              = 16
	updaterUpdate                = 17
	musicPutTitle                = 7
	musicPutArtist               = 11
	music2PutAlbumTitle          = 7
	timelinePutEndTime           = 9
	timelinePutMaxSeekTime       = 13
	timelinePutPosition          = 15
	streamRefCreateFromStream    = 8
	buttonArgsGetButton          = 6
	positionArgsGetRequestedTime = 6

	mediaPlaybackStatusClosed  = 0
	mediaPlaybackStatusStopped = 2
	mediaPlaybackStatusPlaying = 3
	mediaPlaybackStatusPaused  = 4
	mediaPlaybackTypeMusic     = 1
)

// smtcButton is the command of a SystemMediaTransportControlsButton.
func smtcButton(button int32) platform.MediaCommand {
	switch button {
	case 0:
		return platform.MediaPlay
	case 1:
		return platform.MediaPause
	case 2:
		return platform.MediaStop
	case 6:
		return platform.MediaNext
	case 7:
		return platform.MediaPrevious
	}
	return 0
}

// The controls, main thread only.
var smtc struct {
	controls, controls2 uintptr
	failed              bool
}

// hstring returns a new HSTRING of s, which the caller deletes.
func hstring(s string) uintptr {
	u := utf16Units(s)
	if len(u) == 0 {
		return 0
	}
	var h uintptr
	if r, _, _ := winrt().createString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)), uintptr(unsafe.Pointer(&h))); failed(r) {
		return 0
	}
	return h
}

func deleteHString(h uintptr) {
	if h != 0 {
		winrt().deleteString.Call(h)
	}
}

// withHString passes a temporary HSTRING of s to call.
func withHString(s string, call func(h uintptr)) {
	h := hstring(s)
	defer deleteHString(h)
	call(h)
}

// activationFactory returns the activation factory of a Windows Runtime
// class as iid, or 0.
func activationFactory(class string, iid *GUID) uintptr {
	var f uintptr
	withHString(class, func(h uintptr) {
		if r, _, _ := winrt().getActivationFactory.Call(h, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&f))); failed(r) {
			f = 0
		}
	})
	return f
}

// activate creates an instance of a Windows Runtime class, as its
// IInspectable, or 0.
func activate(class string) uintptr {
	var obj uintptr
	withHString(class, func(h uintptr) {
		if r, _, _ := winrt().activateInstance.Call(h, uintptr(unsafe.Pointer(&obj))); failed(r) {
			obj = 0
		}
	})
	return obj
}

var (
	smtcHandlerVtbl [4]uintptr
	smtcHandlerIIDs []GUID
	smtcHandlerOnce sync.Once
)

// newSMTCHandler is newHandler for the events of the controls, which call
// agile delegates on threads of their own instead of marshaling the call
// to the main thread: fn must be safe on any thread. The handler is only
// IUnknown, IAgileObject and the controls' delegates,
// TypedEventHandler<SystemMediaTransportControls, …ButtonPressedEventArgs>
// and <…, PlaybackPositionChangeRequestedEventArgs>; a source asking for
// more, as IWeakReferenceSource, must not get the handler's Invoke.
func newSMTCHandler(fn func(a, b uintptr)) uintptr {
	smtcHandlerOnce.Do(func() {
		smtcHandlerIIDs = []GUID{
			guid("00000000-0000-0000-c000-000000000046"), // IUnknown
			iidIAgileObject,
			guid("0557e996-7b23-5bae-aa81-ea0d671143a4"),
			guid("44e34f15-bdc0-50a7-ace4-39e91fb753f1"),
		}
		smtcHandlerVtbl = handlerVtbl
		smtcHandlerVtbl[0] = syscall.NewCallback(func(this, riid, out uintptr) uintptr {
			if !slices.Contains(smtcHandlerIIDs, *(*GUID)(native(riid))) {
				*(*uintptr)(native(out)) = 0
				return eNoInterface
			}
			*(*uintptr)(native(out)) = this
			handlerRef(this, 1)
			return sOK
		})
	})
	h := newHandler(fn)
	(*handler)(native(h)).vtbl = &smtcHandlerVtbl
	return h
}

// mediaControls returns the controls of the application window, made and
// hooked up on first use, or 0.
func (b *Backend) mediaControls() uintptr {
	if smtc.controls != 0 || smtc.failed {
		return smtc.controls
	}
	smtc.failed = true
	w := winrt()
	interop := activationFactory("Windows.Media.SystemMediaTransportControls", &w.iidInterop)
	if interop == 0 {
		return 0
	}
	defer release(interop)
	var c uintptr
	if failed(comCall(interop, smtcInteropGetForWindow, b.appHwnd, uintptr(unsafe.Pointer(&w.iidControls)), uintptr(unsafe.Pointer(&c)))) || c == 0 {
		return 0
	}
	report := func(cmd platform.MediaCommand, pos time.Duration) {
		b.post(func() { b.h.MediaCommand(cmd, pos) })
	}
	var token int64
	h := newSMTCHandler(func(_, args uintptr) {
		var button int32
		if !failed(comCall(args, buttonArgsGetButton, uintptr(unsafe.Pointer(&button)))) {
			if cmd := smtcButton(button); cmd != 0 {
				report(cmd, 0)
			}
		}
	})
	comCall(c, smtcAddButtonPressed, h, uintptr(unsafe.Pointer(&token)))
	release(h)
	if c2 := queryInterface(c, &w.iidControls2); c2 != 0 {
		h := newSMTCHandler(func(_, args uintptr) {
			var t int64 // a TimeSpan, in 100 ns
			if !failed(comCall(args, positionArgsGetRequestedTime, uintptr(unsafe.Pointer(&t)))) {
				report(platform.MediaSeek, time.Duration(t)*100)
			}
		})
		comCall(c2, smtc2AddPlaybackPositionChangeRequested, h, uintptr(unsafe.Pointer(&token)))
		release(h)
		smtc.controls2 = c2
	}
	smtc.controls, smtc.failed = c, false
	return c
}

func setNowPlaying(b *Backend, info *platform.NowPlayingInfo) {
	c := b.mediaControls()
	if c == 0 {
		return
	}
	w := winrt()
	var updater uintptr
	comCall(c, smtcGetDisplayUpdater, uintptr(unsafe.Pointer(&updater)))
	defer release(updater)
	if info == nil {
		if updater != 0 {
			comCall(updater, updaterClearAll)
			comCall(updater, updaterUpdate)
		}
		comCall(c, smtcPutPlaybackStatus, mediaPlaybackStatusClosed)
		comCall(c, smtcPutIsEnabled, 0)
		return
	}
	comCall(c, smtcPutIsEnabled, 1)
	status := map[string]uintptr{"playing": mediaPlaybackStatusPlaying, "paused": mediaPlaybackStatusPaused, "stopped": mediaPlaybackStatusStopped}[info.State]
	comCall(c, smtcPutPlaybackStatus, status)
	comCall(c, smtcPutIsPlayEnabled, boolArg(info.Commands&platform.MediaPlay != 0))
	comCall(c, smtcPutIsPauseEnabled, boolArg(info.Commands&platform.MediaPause != 0))
	comCall(c, smtcPutIsStopEnabled, boolArg(info.Commands&platform.MediaStop != 0))
	comCall(c, smtcPutIsNextEnabled, boolArg(info.Commands&platform.MediaNext != 0))
	comCall(c, smtcPutIsPreviousEnabled, boolArg(info.Commands&platform.MediaPrevious != 0))
	if updater != 0 {
		comCall(updater, updaterPutType, mediaPlaybackTypeMusic)
		var music uintptr
		if !failed(comCall(updater, updaterGetMusicProperties, uintptr(unsafe.Pointer(&music)))) && music != 0 {
			withHString(info.Title, func(h uintptr) { comCall(music, musicPutTitle, h) })
			withHString(info.Artist, func(h uintptr) { comCall(music, musicPutArtist, h) })
			if music2 := queryInterface(music, &w.iidMusic2); music2 != 0 {
				withHString(info.Album, func(h uintptr) { comCall(music2, music2PutAlbumTitle, h) })
				release(music2)
			}
			release(music)
		}
		thumb := thumbnail(info.Artwork)
		comCall(updater, updaterPutThumbnail, thumb)
		release(thumb)
		comCall(updater, updaterUpdate)
	}
	if c2 := smtc.controls2; c2 != 0 {
		if tl := activate("Windows.Media.SystemMediaTransportControlsTimelineProperties"); tl != 0 {
			if t := queryInterface(tl, &w.iidTimeline); t != 0 {
				// TimeSpans, in 100 ns, from a start of 0.
				end, pos := uintptr(info.Duration/100), uintptr(info.Position/100)
				comCall(t, timelinePutEndTime, end)
				if info.Commands&platform.MediaSeek != 0 {
					comCall(t, timelinePutMaxSeekTime, end)
				}
				comCall(t, timelinePutPosition, pos)
				comCall(c2, smtc2UpdateTimelineProperties, t)
				release(t)
			}
			release(tl)
		}
		comCallFloat(c2, smtc2PutPlaybackRate, info.Rate)
	}
}

// thumbnail returns a RandomAccessStreamReference of an image, or 0.
func thumbnail(img []byte) uintptr {
	w := winrt()
	if len(img) == 0 || !has(w.streamOverStream) {
		return 0
	}
	stream := memStream(img)
	if stream == 0 {
		return 0
	}
	defer release(stream)
	var ras uintptr
	const bsosDefault = 0
	if r, _, _ := w.streamOverStream.Call(stream, bsosDefault, uintptr(unsafe.Pointer(&w.iidStream)), uintptr(unsafe.Pointer(&ras))); failed(r) {
		return 0
	}
	defer release(ras)
	statics := activationFactory("Windows.Storage.Streams.RandomAccessStreamReference", &w.iidStreamRefNew)
	if statics == 0 {
		return 0
	}
	defer release(statics)
	var ref uintptr
	if failed(comCall(statics, streamRefCreateFromStream, ras, uintptr(unsafe.Pointer(&ref)))) {
		return 0
	}
	return ref
}
