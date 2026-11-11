//go:build linux && (amd64 || arm64)

package linux

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/platform"
)

func (b *Backend) NowPlaying() platform.NowPlaying { return nowPlaying{b} }

// nowPlaying is an MPRIS player (org.mpris.MediaPlayer2) on the session
// bus, which GNOME's and KDE's media controls, their lock screens and
// playerctl show and drive, and which the desktops send the media keys
// to. The app owns its bus name only while it shows something, so the
// player leaves the desktop's controls once it shows nothing.
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

const (
	mprisPath    = "/org/mpris/MediaPlayer2"
	mprisRoot    = "org.mpris.MediaPlayer2"
	mprisPlayer  = "org.mpris.MediaPlayer2.Player"
	mprisNoTrack = "/org/mpris/MediaPlayer2/TrackList/NoTrack"
)

const mprisXML = `<node>
  <interface name="org.mpris.MediaPlayer2">
    <method name="Raise"/>
    <method name="Quit"/>
    <property name="CanQuit" type="b" access="read"/>
    <property name="CanRaise" type="b" access="read"/>
    <property name="HasTrackList" type="b" access="read"/>
    <property name="Identity" type="s" access="read"/>
    <property name="DesktopEntry" type="s" access="read"/>
    <property name="SupportedUriSchemes" type="as" access="read"/>
    <property name="SupportedMimeTypes" type="as" access="read"/>
  </interface>
  <interface name="org.mpris.MediaPlayer2.Player">
    <method name="Next"/>
    <method name="Previous"/>
    <method name="Pause"/>
    <method name="PlayPause"/>
    <method name="Stop"/>
    <method name="Play"/>
    <method name="Seek"><arg direction="in" name="Offset" type="x"/></method>
    <method name="SetPosition"><arg direction="in" name="TrackId" type="o"/><arg direction="in" name="Position" type="x"/></method>
    <method name="OpenUri"><arg direction="in" name="Uri" type="s"/></method>
    <signal name="Seeked"><arg name="Position" type="x"/></signal>
    <property name="PlaybackStatus" type="s" access="read"/>
    <property name="Rate" type="d" access="read"/>
    <property name="Metadata" type="a{sv}" access="read"/>
    <property name="Volume" type="d" access="read"/>
    <property name="Position" type="x" access="read"/>
    <property name="MinimumRate" type="d" access="read"/>
    <property name="MaximumRate" type="d" access="read"/>
    <property name="CanGoNext" type="b" access="read"/>
    <property name="CanGoPrevious" type="b" access="read"/>
    <property name="CanPlay" type="b" access="read"/>
    <property name="CanPause" type="b" access="read"/>
    <property name="CanSeek" type="b" access="read"/>
    <property name="CanControl" type="b" access="read"/>
  </interface>
</node>`

// The player's state, main thread only.
var mpris struct {
	conn       ptr
	registered bool
	owner      uint32 // the bus name's owner id, 0 while not owned
	info       *platform.NowPlayingInfo
	at         time.Time // when info.Position was
	track      int       // counts the tracks, for their ids
	art        string    // the artwork's file
	artHash    uint64
}

// GIO's functions of exported objects, bound with the player.
var (
	gDBusNodeInfoNewForXML               func(xml *byte, err *ptr) ptr
	gDBusNodeInfoLookupInterface         func(node ptr, name *byte) ptr
	gDBusConnectionRegisterObject        func(conn ptr, path *byte, iface ptr, vtable unsafe.Pointer, data, free ptr, err *ptr) uint32
	gBusOwnNameOnConnection              func(conn ptr, name *byte, flags int32, acquired, lost, data, free ptr) uint32
	gBusUnownName                        func(id uint32)
	gDBusMethodInvocationReturnValue     func(inv, params ptr)
	gDBusMethodInvocationReturnDBusError func(inv ptr, name, msg *byte)
	gVariantGetInt64                     func(v ptr) int64

	// mprisVtable is the GDBusInterfaceVTable of both interfaces: method
	// calls, property reads, no writes, and GLib's padding.
	mprisVtable [11]ptr
	mprisOnce   sync.Once
	// mprisStrings is the GVariantType of strings, of empty arrays.
	mprisStrings ptr
)

func bindMPRIS() {
	mprisOnce.Do(func() {
		i := libGIO
		mustBind(i, &gDBusNodeInfoNewForXML, "g_dbus_node_info_new_for_xml")
		mustBind(i, &gDBusNodeInfoLookupInterface, "g_dbus_node_info_lookup_interface")
		mustBind(i, &gDBusConnectionRegisterObject, "g_dbus_connection_register_object")
		mustBind(i, &gBusOwnNameOnConnection, "g_bus_own_name_on_connection")
		mustBind(i, &gBusUnownName, "g_bus_unown_name")
		mustBind(i, &gDBusMethodInvocationReturnValue, "g_dbus_method_invocation_return_value")
		mustBind(i, &gDBusMethodInvocationReturnDBusError, "g_dbus_method_invocation_return_dbus_error")
		mustBind(libGLib, &gVariantGetInt64, "g_variant_get_int64")
		mprisStrings = gVariantTypeNew(cs("s"))
		// Created once: the player is registered once per process.
		mprisVtable[0] = purego.NewCallback(func(conn, sender, path, iface, method, params, inv, data ptr) {
			mprisCall(goStr(iface), goStr(method), params, inv)
		})
		mprisVtable[1] = purego.NewCallback(func(conn, sender, path, iface, prop, gerr, data ptr) ptr {
			return mprisProperty(goStr(iface), goStr(prop))
		})
	})
}

func setNowPlaying(b *Backend, info *platform.NowPlayingInfo) {
	conn, err := bus()
	if err != nil {
		return
	}
	bindMPRIS()
	if !mpris.registered {
		var gerr ptr
		node := gDBusNodeInfoNewForXML(cs(mprisXML), &gerr)
		if node == 0 {
			_ = gErr(gerr)
			return
		}
		for _, iface := range []string{mprisRoot, mprisPlayer} {
			if gDBusConnectionRegisterObject(conn, cs(mprisPath), gDBusNodeInfoLookupInterface(node, cs(iface)), unsafe.Pointer(&mprisVtable), 0, 0, &gerr) == 0 {
				_ = gErr(gerr)
				return
			}
		}
		mpris.conn, mpris.registered = conn, true
	}
	old := mpris.info
	if info == nil {
		mpris.info = nil
		if mpris.owner != 0 {
			gBusUnownName(mpris.owner)
			mpris.owner = 0
		}
		mprisSetArt(nil)
		return
	}
	expected := mprisPosition()
	if old == nil || old.Title != info.Title || old.Artist != info.Artist || old.Album != info.Album || old.Duration != info.Duration {
		mpris.track++
	}
	mpris.info, mpris.at = info, time.Now()
	mprisSetArt(info.Artwork)
	if mpris.owner == 0 {
		name := fmt.Sprintf("org.mpris.MediaPlayer2.%s.instance%d", busNameElement(appName()), os.Getpid())
		mpris.owner = gBusOwnNameOnConnection(conn, cs(name), 0, 0, 0, 0, 0)
		return
	}
	mprisChanged(mprisPlayer, "PlaybackStatus", "Rate", "Metadata", "CanGoNext", "CanGoPrevious", "CanPlay", "CanPause", "CanSeek")
	// Clients advance the position at the rate, and hear only of jumps.
	if d := info.Position - expected; old != nil && (d > time.Second/2 || d < -time.Second/2) {
		var gerr ptr
		if !gDBusConnectionEmitSignal(conn, nil, cs(mprisPath), cs(mprisPlayer), cs("Seeked"), tuple(gVariantNewInt64(info.Position.Microseconds())), &gerr) {
			_ = gErr(gerr)
		}
	}
}

// busNameElement makes s an element of a bus name: ASCII letters,
// digits, _ and -, not starting with a digit.
func busNameElement(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_', r == '-', r >= '0' && r <= '9' && b.Len() > 0:
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteString("_")
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "mygo"
	}
	return b.String()
}

// mprisSetArt writes the artwork to a file, whose URL the metadata gives,
// and removes the one before. Clients read it whenever they like, so the
// file stays until the artwork changes.
func mprisSetArt(art []byte) {
	h := fnv.New64a()
	h.Write(art)
	sum := h.Sum64()
	if len(art) > 0 && mpris.art != "" && sum == mpris.artHash {
		return
	}
	if mpris.art != "" {
		os.Remove(mpris.art)
		mpris.art = ""
	}
	if len(art) == 0 {
		return
	}
	// The user's runtime directory goes away as the session ends.
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, busNameElement(appName()))
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	path := filepath.Join(dir, fmt.Sprintf("now-playing-%d-%x", os.Getpid(), sum))
	if os.WriteFile(path, art, 0o600) == nil {
		mpris.art, mpris.artHash = path, sum
	}
}

// mprisPosition is where playback is now.
func mprisPosition() time.Duration {
	info := mpris.info
	if info == nil {
		return 0
	}
	pos := info.Position
	if info.State == "playing" {
		pos += time.Duration(float64(time.Since(mpris.at)) * info.Rate)
	}
	if info.Duration > 0 {
		pos = min(pos, info.Duration)
	}
	return max(pos, 0)
}

func mprisTrackID() string {
	if mpris.info == nil {
		return mprisNoTrack
	}
	return fmt.Sprintf("/org/mygo/track/%d", mpris.track)
}

// mprisCan reports whether the player offers cmd.
func mprisCan(cmd platform.MediaCommand) bool {
	return mpris.info != nil && mpris.info.Commands&cmd != 0
}

// mprisProperty returns a property's value, a new GVariant.
func mprisProperty(iface, prop string) ptr {
	info := mpris.info
	switch prop {
	case "CanQuit", "CanRaise", "HasTrackList":
		return gVariantNewBoolean(false)
	case "Identity":
		return gVariantNewString(cs(appName()))
	case "DesktopEntry":
		return gVariantNewString(cs(strings.TrimSuffix(desktopEntryID(), ".desktop")))
	case "SupportedUriSchemes", "SupportedMimeTypes":
		return gVariantNewArray(mprisStrings, nil, 0)
	case "PlaybackStatus":
		status := "Stopped"
		if info != nil {
			status = strings.ToUpper(info.State[:1]) + info.State[1:]
		}
		return gVariantNewString(cs(status))
	case "Rate":
		if info != nil {
			return gVariantNewDouble(info.Rate)
		}
		return gVariantNewDouble(1)
	case "Volume", "MinimumRate", "MaximumRate":
		return gVariantNewDouble(1)
	case "Position":
		return gVariantNewInt64(mprisPosition().Microseconds())
	case "Metadata":
		return mprisMetadata()
	case "CanGoNext":
		return gVariantNewBoolean(mprisCan(platform.MediaNext))
	case "CanGoPrevious":
		return gVariantNewBoolean(mprisCan(platform.MediaPrevious))
	case "CanPlay":
		return gVariantNewBoolean(mprisCan(platform.MediaPlay))
	case "CanPause":
		return gVariantNewBoolean(mprisCan(platform.MediaPause))
	case "CanSeek":
		return gVariantNewBoolean(mprisCan(platform.MediaSeek) && info.Duration > 0)
	case "CanControl":
		return gVariantNewBoolean(true)
	}
	return 0
}

func mprisMetadata() ptr {
	entry := func(key string, v ptr) ptr {
		return gVariantNewDictEntry(gVariantNewString(cs(key)), gVariantNewVariant(v))
	}
	entries := []ptr{entry("mpris:trackid", gVariantNewObjectPath(cs(mprisTrackID())))}
	if info := mpris.info; info != nil {
		if info.Title != "" {
			entries = append(entries, entry("xesam:title", gVariantNewString(cs(info.Title))))
		}
		if info.Artist != "" {
			artist := gVariantNewString(cs(info.Artist))
			entries = append(entries, entry("xesam:artist", gVariantNewArray(0, unsafe.Pointer(&artist), 1)))
		}
		if info.Album != "" {
			entries = append(entries, entry("xesam:album", gVariantNewString(cs(info.Album))))
		}
		if info.Duration > 0 {
			entries = append(entries, entry("mpris:length", gVariantNewInt64(info.Duration.Microseconds())))
		}
		if mpris.art != "" {
			entries = append(entries, entry("mpris:artUrl", gVariantNewString(cs("file://"+mpris.art))))
		}
	}
	return gVariantNewArray(0, unsafe.Pointer(&entries[0]), uintptr(len(entries)))
}

// mprisChanged emits PropertiesChanged with the values of props.
func mprisChanged(iface string, props ...string) {
	entries := make([]ptr, len(props))
	for i, p := range props {
		entries[i] = gVariantNewDictEntry(gVariantNewString(cs(p)), gVariantNewVariant(mprisProperty(iface, p)))
	}
	params := tuple(
		gVariantNewString(cs(iface)),
		gVariantNewArray(0, unsafe.Pointer(&entries[0]), uintptr(len(entries))),
		gVariantNewArray(mprisStrings, nil, 0),
	)
	var gerr ptr
	if !gDBusConnectionEmitSignal(mpris.conn, nil, cs(mprisPath), cs("org.freedesktop.DBus.Properties"), cs("PropertiesChanged"), params, &gerr) {
		_ = gErr(gerr)
	}
}

// mprisCall answers a method call, reporting the player's commands.
func mprisCall(iface, method string, params, inv ptr) {
	var cmd platform.MediaCommand
	var pos time.Duration
	switch method {
	case "Play":
		cmd = platform.MediaPlay
	case "Pause":
		cmd = platform.MediaPause
	case "PlayPause":
		cmd = platform.MediaTogglePlayPause
	case "Stop":
		cmd = platform.MediaStop
	case "Next":
		cmd = platform.MediaNext
	case "Previous":
		cmd = platform.MediaPrevious
	case "Seek":
		child := gVariantGetChildValue(params, 0)
		offset := time.Duration(gVariantGetInt64(child)) * time.Microsecond
		gVariantUnref(child)
		cmd, pos = platform.MediaSeek, max(mprisPosition()+offset, 0)
	case "SetPosition":
		track := gVariantGetChildValue(params, 0)
		id := goStr(gVariantGetString(track, nil))
		gVariantUnref(track)
		child := gVariantGetChildValue(params, 1)
		pos = time.Duration(gVariantGetInt64(child)) * time.Microsecond
		gVariantUnref(child)
		// The specification ignores positions of other tracks, and out
		// of the track.
		if info := mpris.info; id == mprisTrackID() && info != nil && pos >= 0 && (info.Duration == 0 || pos <= info.Duration) {
			cmd = platform.MediaSeek
		}
	case "OpenUri":
		gDBusMethodInvocationReturnDBusError(inv, cs("org.freedesktop.DBus.Error.NotSupported"), cs("the player opens no URIs"))
		return
	}
	gDBusMethodInvocationReturnValue(inv, 0)
	if cmd != 0 && mpris.info != nil {
		theBackend.h.MediaCommand(cmd, pos)
	}
}
