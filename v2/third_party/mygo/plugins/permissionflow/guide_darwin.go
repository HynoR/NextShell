//go:build darwin

package permissionflow

import (
	"context"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo"
)

// Guide's panel is an NSPanel that never takes the focus, so that System
// Settings stays the active app while the user drags. A goroutine follows
// System Settings through the window server (CGWindowListCopyWindowInfo),
// which needs no permission, 30 times a second, and moves the panel on the
// main thread when the window has moved, or hides it while another app is
// in front. The card of the app is an NSView of a class of ours, the
// source of the drag, whose item is a writer of ours putting the app on
// the pasteboard as Finder does.

type nsPoint struct{ X, Y float64 }
type nsSize struct{ Width, Height float64 }
type nsRect struct {
	Origin nsPoint
	Size   nsSize
}

func (r rect) ns() nsRect { return nsRect{nsPoint{r.X, r.Y}, nsSize{r.W, r.H}} }

func fromNS(r nsRect) rect { return rect{r.Origin.X, r.Origin.Y, r.Size.Width, r.Size.Height} }

const (
	nsWindowStyleMaskBorderless         = 0
	nsWindowStyleMaskNonactivatingPanel = 1 << 7
	nsBackingStoreBuffered              = 2
	nsFloatingWindowLevel               = 3
	nsCollectionCanJoinAllSpaces        = 1 << 0
	nsCollectionFullScreenAuxiliary     = 1 << 8
	nsVisualEffectMaterialPopover       = 6
	nsVisualEffectBlendingBehindWindow  = 0
	nsVisualEffectStateActive           = 1
	nsBoxCustom                         = 4
	nsNoTitle                           = 0
	nsDragOperationCopy                 = 1
	nsDraggingFormationNone             = 1
	nsImageOnly                         = 1
	nsImageResizingModeStretch          = 1
	nsTextAlignmentCenter               = 2
	kCGWindowListOptionOnScreenOnly     = 1 << 0
	kCGWindowListExcludeDesktopElements = 1 << 4
	kCGEventSourceStateCombined         = 0
	kCGMouseButtonLeft                  = 0

	// The panel's layout, in points.
	panelPadding = 12
	panelRadius  = 16
	cardHeight   = 48
	iconSize     = 32
	closeSize    = 18
	dragIconSize = 56

	// trackInterval is how often the panel follows System Settings.
	trackInterval = time.Second / 30
	// settingsMisses is how many times in a row System Settings must be
	// missing for the panel to close: it is missing for a moment while it
	// opens or changes pages.
	settingsMisses = 12
	// settingsWait is how long System Settings may take to open.
	settingsWait = 10 * time.Second
)

var (
	classesOnce sync.Once
	classesErr  error

	// active is the flow whose panel shows, which the panel's classes
	// reach; one shows at a time. Main thread only, like the panel.
	active *flow
	// dragging is set while the app is dragged: the panel lets the mouse
	// through, and the tracker restores it once the button is up.
	dragging atomic.Bool

	flowMu   sync.Mutex
	lastFlow *flow // the flow running, which a new one stops
)

// A flow is one call of Guide.
type flow struct {
	p          Permission
	app, name  string
	draggable  bool
	stop       chan struct{} // closed when the user closes it, or another flow starts
	stopOnce   sync.Once
	panel      objc.ID
	views      panelViews
	labelH     float64
	shown      bool
	dragOrigin nsPoint
	dragBegan  bool
}

// panelViews are the views of the panel, which its content view keeps.
type panelViews struct {
	border, label, close, cardBox, icon, name, hint, hand, card objc.ID
}

func (f *flow) close() { f.stopOnce.Do(func() { close(f.stop) }) }

func registerClasses() error {
	classesOnce.Do(func() {
		source := objc.GetProtocol("NSDraggingSource")
		writing := objc.GetProtocol("NSPasteboardWriting")
		var protocols []*objc.Protocol
		if source != nil {
			protocols = append(protocols, source)
		}
		_, classesErr = objc.RegisterClass("MyGoPermissionFlowDragView", objc.GetClass("NSView"), protocols, nil, []objc.MethodDef{
			{Cmd: sel("mouseDown:"), Fn: cardMouseDown},
			{Cmd: sel("mouseDragged:"), Fn: cardMouseDragged},
			{Cmd: sel("mouseUp:"), Fn: cardMouseUp},
			{Cmd: sel("acceptsFirstMouse:"), Fn: func(objc.ID, objc.SEL, objc.ID) bool { return true }},
			{Cmd: sel("mouseDownCanMoveWindow"), Fn: func(objc.ID, objc.SEL) bool { return false }},
			{Cmd: sel("resetCursorRects"), Fn: cardResetCursorRects},
			{Cmd: sel("draggingSession:sourceOperationMaskForDraggingContext:"), Fn: func(objc.ID, objc.SEL, objc.ID, int) uint { return nsDragOperationCopy }},
			{Cmd: sel("ignoreModifierKeysForDraggingSession:"), Fn: func(objc.ID, objc.SEL, objc.ID) bool { return true }},
		})
		if classesErr != nil {
			return
		}
		protocols = nil
		if writing != nil {
			protocols = append(protocols, writing)
		}
		_, classesErr = objc.RegisterClass("MyGoPermissionFlowAppWriter", objc.GetClass("NSObject"), protocols, nil, []objc.MethodDef{
			{Cmd: sel("writableTypesForPasteboard:"), Fn: writerTypes},
			{Cmd: sel("pasteboardPropertyListForType:"), Fn: writerPropertyList},
		})
		if classesErr != nil {
			return
		}
		_, classesErr = objc.RegisterClass("MyGoPermissionFlowTarget", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{Cmd: sel("close:"), Fn: func(objc.ID, objc.SEL, objc.ID) {
				if active != nil {
					active.close()
				}
			}},
		})
	})
	return classesErr
}

// The pasteboard types System Settings takes an app dragged from Finder
// with.
var appTypes = []string{
	"public.file-url",
	"public.url",
	"NSFilenamesPboardType",
	"com.apple.pasteboard.promised-file-url",
	"public.utf8-plain-text",
}

func writerTypes(_ objc.ID, _ objc.SEL, _ objc.ID) objc.ID {
	arr := class("NSMutableArray").Send(sel("array"))
	for _, t := range appTypes {
		arr.Send(sel("addObject:"), nsString(t))
	}
	return arr
}

func writerPropertyList(_ objc.ID, _ objc.SEL, typ objc.ID) objc.ID {
	if active == nil {
		return 0
	}
	path := active.app
	switch goString(typ) {
	case "public.file-url", "public.url", "com.apple.pasteboard.promised-file-url":
		u := class("NSURL").Send(sel("fileURLWithPath:"), nsString(path))
		return u.Send(sel("absoluteString"))
	case "NSFilenamesPboardType":
		return class("NSArray").Send(sel("arrayWithObject:"), nsString(path))
	case "public.utf8-plain-text":
		return nsString(path)
	}
	return 0
}

func cardResetCursorRects(self objc.ID, _ objc.SEL) {
	bounds := objc.Send[nsRect](self, sel("bounds"))
	self.Send(sel("addCursorRect:cursor:"), bounds, class("NSCursor").Send(sel("openHandCursor")))
}

func cardMouseDown(self objc.ID, _ objc.SEL, event objc.ID) {
	if active == nil {
		return
	}
	active.dragOrigin = objc.Send[nsPoint](event, sel("locationInWindow"))
	active.dragBegan = false
}

func cardMouseUp(objc.ID, objc.SEL, objc.ID) {
	if active != nil {
		active.dragBegan = false
	}
}

func cardMouseDragged(self objc.ID, _ objc.SEL, event objc.ID) {
	f := active
	if f == nil || f.dragBegan {
		return
	}
	at := objc.Send[nsPoint](event, sel("locationInWindow"))
	if math.Hypot(at.X-f.dragOrigin.X, at.Y-f.dragOrigin.Y) <= 4 {
		return
	}
	f.dragBegan = true
	writer := class("MyGoPermissionFlowAppWriter").Send(sel("alloc")).Send(sel("init"))
	item := class("NSDraggingItem").Send(sel("alloc")).Send(sel("initWithPasteboardWriter:"), writer)
	icon := class("NSWorkspace").Send(sel("sharedWorkspace")).Send(sel("iconForFile:"), nsString(f.app))
	icon.Send(sel("setSize:"), nsSize{dragIconSize, dragIconSize})
	p := objc.Send[nsPoint](self, sel("convertPoint:fromView:"), at, objc.ID(0))
	item.Send(sel("setDraggingFrame:contents:"), nsRect{nsPoint{p.X - dragIconSize/2, p.Y - dragIconSize/2}, nsSize{dragIconSize, dragIconSize}}, icon)
	session := self.Send(sel("beginDraggingSessionWithItems:event:source:"), class("NSArray").Send(sel("arrayWithObject:"), item), event, self)
	session.Send(sel("setAnimatesToStartingPositionsOnCancelOrFail:"), true)
	session.Send(sel("setDraggingFormation:"), nsDraggingFormationNone)
	item.Send(sel("release"))
	writer.Send(sel("release"))
	// System Settings, under the pointer, takes the drop.
	dragging.Store(true)
	f.panel.Send(sel("setIgnoresMouseEvents:"), true)
	f.panel.Send(sel("setAlphaValue:"), 0.72)
}

// endDrag restores the panel after a drag. Main thread.
func (f *flow) endDrag() {
	dragging.Store(false)
	f.dragBegan = false
	if f.panel != 0 {
		f.panel.Send(sel("setIgnoresMouseEvents:"), false)
		f.panel.Send(sel("setAlphaValue:"), 1.0)
	}
}

func defaultApp() string {
	var path string
	withPool(func() {
		path = goString(class("NSBundle").Send(sel("mainBundle")).Send(sel("bundlePath")))
	})
	if strings.HasSuffix(path, ".app") {
		return path
	}
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return path
}

func guide(ctx context.Context, p Permission, opts GuideOptions) (Status, error) {
	if err := loadCore(); err != nil {
		return Unknown, err
	}
	if err := registerClasses(); err != nil {
		return Unknown, err
	}
	f := &flow{p: p, app: opts.App, name: opts.Name, draggable: macDraggable(p), stop: make(chan struct{})}
	if f.app == "" {
		f.app = defaultApp()
	}
	if f.name == "" {
		f.name = appName(f.app)
	}
	flowMu.Lock()
	if lastFlow != nil {
		lastFlow.close()
	}
	lastFlow = f
	flowMu.Unlock()
	defer func() {
		flowMu.Lock()
		if lastFlow == f {
			lastFlow = nil
		}
		flowMu.Unlock()
		mygo.RunOnMain(f.destroy)
	}()

	if err := openSettings(p); err != nil {
		return Unknown, err
	}
	var (
		t       = time.NewTicker(trackInterval)
		started = time.Now()
		seen    bool // System Settings was there
		misses  int  // times in a row it wasn't
		last    rect // where its window was
		front   bool // whether it was in front
		ticks   int
	)
	defer t.Stop()
	for {
		if ticks%int(pollInterval/trackInterval) == 0 {
			if st, err := check(p); err != nil || st == Granted {
				return st, err
			}
		}
		ticks++

		pid, frontmost := settingsProcess()
		switch {
		case pid != 0:
			seen, misses = true, 0
		case seen:
			if misses++; misses >= settingsMisses {
				return check(p)
			}
		case time.Since(started) > settingsWait:
			return check(p)
		}
		if dragging.Load() && !cgEventSourceButtonState(kCGEventSourceStateCombined, kCGMouseButtonLeft) {
			mygo.RunOnMain(f.endDrag)
		}
		if f.draggable && pid != 0 {
			// While a drag is on, System Settings is in front even when the
			// window server doesn't say so yet.
			show := frontmost || dragging.Load()
			if frame, ok := settingsFrame(pid); ok && (frame != last || show != front) {
				last, front = frame, show
				mygo.RunOnMain(func() { f.place(frame, show) })
			}
		}

		select {
		case <-ctx.Done():
			st, _ := check(p)
			return st, ctx.Err()
		case <-f.stop:
			return check(p)
		case <-t.C:
		}
	}
}

// settingsProcess returns the process of System Settings, 0 when it isn't
// running, and whether it is the app in front.
func settingsProcess() (pid int32, frontmost bool) {
	withPool(func() {
		apps := class("NSRunningApplication").Send(sel("runningApplicationsWithBundleIdentifier:"), nsString(settingsBundleID))
		if sendUint(apps, sel("count")) == 0 {
			return
		}
		app := apps.Send(sel("firstObject"))
		pid = int32(sendInt(app, sel("processIdentifier")))
		front := class("NSWorkspace").Send(sel("sharedWorkspace")).Send(sel("frontmostApplication"))
		frontmost = front != 0 && int32(sendInt(front, sel("processIdentifier"))) == pid
	})
	return pid, frontmost
}

// settingsFrame returns the bounds of System Settings' window, in the
// window server's coordinates.
func settingsFrame(pid int32) (r rect, ok bool) {
	withPool(func() {
		list := cgWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly|kCGWindowListExcludeDesktopElements, 0)
		if list == 0 {
			return
		}
		defer cfRelease(list)
		var (
			kPID    = nsString("kCGWindowOwnerPID")
			kLayer  = nsString("kCGWindowLayer")
			kAlpha  = nsString("kCGWindowAlpha")
			kBounds = nsString("kCGWindowBounds")
			windows []settingsWindow
		)
		n := sendUint(list, sel("count"))
		for i := range n {
			w := list.Send(sel("objectAtIndex:"), i)
			if v := w.Send(sel("objectForKey:"), kPID); v == 0 || int32(sendInt(v, sel("integerValue"))) != pid {
				continue
			}
			sw := settingsWindow{alpha: 1}
			if v := w.Send(sel("objectForKey:"), kLayer); v != 0 {
				sw.layer = sendInt(v, sel("integerValue"))
			}
			if v := w.Send(sel("objectForKey:"), kAlpha); v != 0 {
				sw.alpha = sendFloat(v, sel("doubleValue"))
			}
			b := w.Send(sel("objectForKey:"), kBounds)
			if b == 0 {
				continue
			}
			num := func(k string) float64 {
				v := b.Send(sel("objectForKey:"), nsString(k))
				if v == 0 {
					return 0
				}
				return sendFloat(v, sel("doubleValue"))
			}
			sw.bounds = rect{num("X"), num("Y"), num("Width"), num("Height")}
			windows = append(windows, sw)
		}
		r, ok = mainSettingsWindow(windows)
	})
	return r, ok
}

// place shows the panel under the window of System Settings at frame (in
// the window server's coordinates), or hides it. Main thread.
func (f *flow) place(frame rect, show bool) {
	if !show {
		if f.panel != 0 && f.shown {
			f.panel.Send(sel("orderOut:"), objc.ID(0))
			f.shown = false
		}
		return
	}
	withPool(func() {
		screens := class("NSScreen").Send(sel("screens"))
		n := objc.Send[uint](screens, sel("count"))
		if n == 0 {
			return
		}
		mainHeight := objc.Send[nsRect](screens.Send(sel("objectAtIndex:"), uint(0)), sel("frame")).Size.Height
		settings := flipRect(frame, mainHeight)
		var visible []rect
		for i := range n {
			visible = append(visible, fromNS(objc.Send[nsRect](screens.Send(sel("objectAtIndex:"), i), sel("visibleFrame"))))
		}
		screen := screenOf(settings, visible)
		w := panelWidth(settings, screen)
		if f.panel == 0 {
			f.build(w)
		}
		f.layout(w)
		h := panelPadding + f.labelH + 8 + cardHeight + panelPadding
		target := panelFrame(settings, screen, h)
		f.panel.Send(sel("setFrame:display:"), target.ns(), true)
		f.panel.Send(sel("invalidateShadow"))
		if !f.shown {
			f.panel.Send(sel("orderFrontRegardless"))
			f.shown = true
		}
		active = f
	})
}

func (f *flow) build(w float64) {
	panel := class("NSPanel").Send(sel("alloc")).Send(sel("initWithContentRect:styleMask:backing:defer:"),
		nsRect{Size: nsSize{w, 100}}, uint(nsWindowStyleMaskBorderless|nsWindowStyleMaskNonactivatingPanel), uint(nsBackingStoreBuffered), false)
	panel.Send(sel("setLevel:"), nsFloatingWindowLevel)
	panel.Send(sel("setReleasedWhenClosed:"), false)
	panel.Send(sel("setOpaque:"), false)
	panel.Send(sel("setBackgroundColor:"), class("NSColor").Send(sel("clearColor")))
	panel.Send(sel("setHasShadow:"), true)
	panel.Send(sel("setHidesOnDeactivate:"), false)
	panel.Send(sel("setCollectionBehavior:"), uint(nsCollectionCanJoinAllSpaces|nsCollectionFullScreenAuxiliary))
	panel.Send(sel("setMovableByWindowBackground:"), false)

	effect := class("NSVisualEffectView").Send(sel("alloc")).Send(sel("initWithFrame:"), nsRect{})
	effect.Send(sel("setMaterial:"), nsVisualEffectMaterialPopover)
	effect.Send(sel("setBlendingMode:"), nsVisualEffectBlendingBehindWindow)
	effect.Send(sel("setState:"), nsVisualEffectStateActive)
	// A layer's corner radius doesn't clip what the view blurs from behind
	// the window: a mask does, and the window's shadow follows it.
	mask := roundedMask(panelRadius)
	effect.Send(sel("setMaskImage:"), mask)
	mask.Send(sel("release"))
	panel.Send(sel("setContentView:"), effect)
	effect.Send(sel("release"))

	color := func(name string) objc.ID { return class("NSColor").Send(sel(name)) }
	v := &f.views
	v.border = newBox(color("clearColor"), panelRadius)
	effect.Send(sel("addSubview:"), v.border)
	v.border.Send(sel("release"))

	label := class("NSTextField").Send(sel("wrappingLabelWithString:"), nsString(""))
	text := class("NSMutableAttributedString").Send(sel("alloc")).Send(sel("init"))
	for _, r := range panelText(f.name, f.p.Title()) {
		font := class("NSFont").Send(sel("systemFontOfSize:"), 13.0)
		if r.bold {
			font = class("NSFont").Send(sel("boldSystemFontOfSize:"), 13.0)
		}
		attrs := class("NSMutableDictionary").Send(sel("dictionary"))
		attrs.Send(sel("setObject:forKey:"), font, nsString("NSFont"))
		attrs.Send(sel("setObject:forKey:"), color("labelColor"), nsString("NSColor"))
		part := class("NSAttributedString").Send(sel("alloc")).Send(sel("initWithString:attributes:"), nsString(r.text), attrs)
		text.Send(sel("appendAttributedString:"), part)
		part.Send(sel("release"))
	}
	label.Send(sel("setAttributedStringValue:"), text)
	text.Send(sel("release"))
	effect.Send(sel("addSubview:"), label)
	v.label = label

	closeButton := class("NSButton").Send(sel("alloc")).Send(sel("initWithFrame:"), nsRect{})
	closeButton.Send(sel("setBordered:"), false)
	closeButton.Send(sel("setImagePosition:"), nsImageOnly)
	if img := symbol("xmark.circle.fill", "Close"); img != 0 {
		closeButton.Send(sel("setImage:"), img)
		closeButton.Send(sel("setContentTintColor:"), color("secondaryLabelColor"))
	} else {
		closeButton.Send(sel("setTitle:"), nsString("✕"))
	}
	target := class("MyGoPermissionFlowTarget").Send(sel("alloc")).Send(sel("init")) // kept, as small as it is
	closeButton.Send(sel("setTarget:"), target)
	closeButton.Send(sel("setAction:"), sel("close:"))
	effect.Send(sel("addSubview:"), closeButton)
	closeButton.Send(sel("release"))
	v.close = closeButton

	// The card: a box, the app's icon and name, a hint, and over them the
	// view the drag starts from, which gets every click on the card.
	cardBox := newBox(color("controlBackgroundColor").Send(sel("colorWithAlphaComponent:"), 0.65), 10)
	effect.Send(sel("addSubview:"), cardBox)
	cardBox.Send(sel("release"))
	v.cardBox = cardBox

	icon := class("NSImageView").Send(sel("imageViewWithImage:"),
		class("NSWorkspace").Send(sel("sharedWorkspace")).Send(sel("iconForFile:"), nsString(f.app)))
	effect.Send(sel("addSubview:"), icon)
	v.icon = icon

	name := class("NSTextField").Send(sel("labelWithString:"), nsString(f.name))
	name.Send(sel("setFont:"), class("NSFont").Send(sel("systemFontOfSize:"), 15.0))
	effect.Send(sel("addSubview:"), name)
	v.name = name

	hint := class("NSTextField").Send(sel("labelWithString:"), nsString("Drag"))
	hint.Send(sel("setFont:"), class("NSFont").Send(sel("systemFontOfSize:"), 9.0))
	hint.Send(sel("setTextColor:"), color("secondaryLabelColor"))
	hint.Send(sel("setAlignment:"), nsTextAlignmentCenter)
	effect.Send(sel("addSubview:"), hint)
	v.hint = hint

	if img := symbol("hand.draw", "Drag"); img != 0 {
		hand := class("NSImageView").Send(sel("imageViewWithImage:"), img)
		hand.Send(sel("setContentTintColor:"), color("secondaryLabelColor"))
		effect.Send(sel("addSubview:"), hand)
		v.hand = hand
	}

	card := class("MyGoPermissionFlowDragView").Send(sel("alloc")).Send(sel("initWithFrame:"), nsRect{})
	effect.Send(sel("addSubview:"), card)
	card.Send(sel("release"))
	v.card = card
	f.panel = panel
}

// roundedMask returns an image of a rounded rectangle that stretches to
// any size, keeping its corners, for NSVisualEffectView's maskImage.
func roundedMask(radius float64) objc.ID {
	side := 2*radius + 1
	img := class("NSImage").Send(sel("alloc")).Send(sel("initWithSize:"), nsSize{side, side})
	img.Send(sel("lockFocus"))
	class("NSColor").Send(sel("blackColor")).Send(sel("set"))
	class("NSBezierPath").Send(sel("bezierPathWithRoundedRect:xRadius:yRadius:"), nsRect{Size: nsSize{side, side}}, radius, radius).Send(sel("fill"))
	img.Send(sel("unlockFocus"))
	type nsEdgeInsets struct{ Top, Left, Bottom, Right float64 }
	img.Send(sel("setCapInsets:"), nsEdgeInsets{radius, radius, radius, radius})
	img.Send(sel("setResizingMode:"), nsImageResizingModeStretch)
	return img
}

// newBox returns a rounded box with the separator's border, filled.
func newBox(fill objc.ID, radius float64) objc.ID {
	box := class("NSBox").Send(sel("alloc")).Send(sel("initWithFrame:"), nsRect{})
	box.Send(sel("setBoxType:"), nsBoxCustom)
	box.Send(sel("setTitlePosition:"), nsNoTitle)
	box.Send(sel("setBorderWidth:"), 1.0)
	box.Send(sel("setCornerRadius:"), radius)
	box.Send(sel("setBorderColor:"), class("NSColor").Send(sel("separatorColor")))
	box.Send(sel("setFillColor:"), fill)
	return box
}

// symbol returns an SF Symbol, which macOS 11 and later have, or 0.
func symbol(name, description string) objc.ID {
	img := class("NSImage")
	if !objc.Send[bool](img, sel("respondsToSelector:"), sel("imageWithSystemSymbolName:accessibilityDescription:")) {
		return 0
	}
	return img.Send(sel("imageWithSystemSymbolName:accessibilityDescription:"), nsString(name), nsString(description))
}

// layout lays the panel out for the width, and measures its text.
func (f *flow) layout(w float64) {
	v := &f.views
	textW := w - 2*panelPadding - closeSize - 6
	v.label.Send(sel("setPreferredMaxLayoutWidth:"), textW)
	f.labelH = math.Ceil(objc.Send[nsSize](v.label, sel("fittingSize")).Height)
	h := panelPadding + f.labelH + 8 + cardHeight + panelPadding
	set := func(view objc.ID, r rect) {
		if view != 0 {
			view.Send(sel("setFrame:"), r.ns())
		}
	}
	card := rect{panelPadding, panelPadding, w - 2*panelPadding, cardHeight}
	set(v.border, rect{0, 0, w, h})
	set(v.label, rect{panelPadding, h - panelPadding - f.labelH, textW, f.labelH})
	set(v.close, rect{w - panelPadding - closeSize, h - panelPadding - closeSize + 2, closeSize, closeSize})
	set(v.cardBox, card)
	set(v.icon, rect{card.X + 8, card.Y + (cardHeight-iconSize)/2, iconSize, iconSize})
	set(v.name, rect{card.X + 8 + iconSize + 8, card.Y + (cardHeight-20)/2, card.W - iconSize - 70, 20})
	set(v.hand, rect{card.maxX() - 38, card.Y + 20, 24, 18})
	set(v.hint, rect{card.maxX() - 44, card.Y + 6, 36, 12})
	set(v.card, card)
	f.panel.Send(sel("invalidateCursorRectsForView:"), v.card)
}

// destroy closes the panel. Main thread.
func (f *flow) destroy() {
	if active == f {
		active = nil
		dragging.Store(false)
	}
	if f.panel == 0 {
		return
	}
	f.panel.Send(sel("orderOut:"), objc.ID(0))
	f.panel.Send(sel("close"))
	f.panel.Send(sel("release"))
	f.panel, f.views = 0, panelViews{}
}
