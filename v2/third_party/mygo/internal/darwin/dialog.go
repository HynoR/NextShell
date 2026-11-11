//go:build darwin

package darwin

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/internal/platform"
)

type dialogs struct{ b *Backend }

const nsModalResponseOK = 1

// runPanel shows a panel or alert as a sheet of parent when possible, else
// application modal, and calls done with the modal response.
func runPanel(obj id, parent *window, sheetSel string, done func(resp int)) {
	if parent != nil && !parent.closed && parent.IsVisible() {
		retain(obj)
		blk := newBlock(func(_ objc.Block, resp int) {
			withPool(func() { done(resp) })
			release(obj)
		})
		send(obj, sheetSel, uintptr(parent.win), uintptr(blk))
		blk.Release()
		return
	}
	send(theBackend.app, "activateIgnoringOtherApps:", 1)
	done(sendInt(obj, "runModal"))
	if b := theBackend; b.quitAfterModal {
		b.quitAfterModal = false
		b.Quit()
	}
}

func asWindow(pw platform.Window) *window {
	w, _ := pw.(*window)
	return w
}

// setupSavePanel applies the options shared by open and save panels.
func setupSavePanel(panel id, title, message, button, defaultPath string, filters []platform.FileFilter, hidden, mkdir, packages bool) {
	if title != "" {
		send(panel, "setTitle:", uintptr(nsString(title)))
	}
	if message != "" {
		send(panel, "setMessage:", uintptr(nsString(message)))
	}
	if button != "" {
		send(panel, "setPrompt:", uintptr(nsString(button)))
	}
	send(panel, "setShowsHiddenFiles:", boolArg(hidden))
	send(panel, "setCanCreateDirectories:", boolArg(mkdir))
	send(panel, "setTreatsFilePackagesAsDirectories:", boolArg(packages))
	if defaultPath != "" {
		if st, err := os.Stat(defaultPath); err == nil && st.IsDir() {
			send(panel, "setDirectoryURL:", uintptr(fileURL(defaultPath)))
		} else {
			send(panel, "setDirectoryURL:", uintptr(fileURL(filepath.Dir(defaultPath))))
			send(panel, "setNameFieldStringValue:", uintptr(nsString(filepath.Base(defaultPath))))
		}
	}
	var exts, mimes []string
	for _, f := range filters {
		for _, e := range f.Extensions {
			if e == "*" {
				return
			}
			exts = append(exts, strings.TrimPrefix(e, "."))
		}
		for _, m := range f.MIMETypes {
			if m == "*/*" || m == "*" {
				return
			}
			mimes = append(mimes, strings.ToLower(m))
		}
	}
	if len(exts) == 0 && len(mimes) == 0 {
		return
	}
	if hasClass("UTType") {
		var types []id
		for _, e := range exts {
			if t := send(class("UTType"), "typeWithFilenameExtension:", uintptr(nsString(e))); t != 0 {
				types = append(types, t)
			}
		}
		for _, m := range mimes {
			var t id
			if kind := mimeKinds[m]; kind != "" {
				t = send(class("UTType"), "typeWithIdentifier:", uintptr(nsString(kind)))
			} else {
				t = send(class("UTType"), "typeWithMIMEType:", uintptr(nsString(m)))
			}
			if t != 0 {
				types = append(types, t)
			}
		}
		send(panel, "setAllowedContentTypes:", uintptr(nsArray(types...)))
		return
	}
	// Before macOS 11, file types are extensions or identifiers of types:
	// those of the kinds of MIME types.
	var names []id
	for _, e := range exts {
		names = append(names, nsString(e))
	}
	for _, m := range mimes {
		if kind := mimeKinds[m]; kind != "" {
			names = append(names, nsString(kind))
		}
	}
	if len(names) > 0 {
		send(panel, "setAllowedFileTypes:", uintptr(nsArray(names...)))
	}
}

// mimeKinds are the types of files (UTTypes) that MIME types of a kind
// are, as WebKit takes them for an input's accept.
var mimeKinds = map[string]string{
	"image/*": "public.image",
	"audio/*": "public.audio",
	"video/*": "public.movie",
	"text/*":  "public.text",
	"font/*":  "public.font",
}

func (d dialogs) ShowOpenDialog(parent platform.Window, o *platform.OpenDialogOptions, cb func([]string, error)) {
	withPool(func() {
		panel := send(class("NSOpenPanel"), "openPanel")
		send(panel, "setCanChooseFiles:", boolArg(o.OpenFiles))
		send(panel, "setCanChooseDirectories:", boolArg(o.OpenDirectories))
		send(panel, "setAllowsMultipleSelection:", boolArg(o.Multiple))
		setupSavePanel(panel, o.Title, o.Message, o.ButtonLabel, o.DefaultPath, o.Filters,
			o.ShowHiddenFiles, o.CreateDirectories, o.TreatPackagesAsDirectories)
		runPanel(panel, asWindow(parent), "beginSheetModalForWindow:completionHandler:", func(resp int) {
			if resp != nsModalResponseOK {
				cb(nil, nil)
				return
			}
			var paths []string
			for _, u := range arrayItems(send(panel, "URLs")) {
				paths = append(paths, goString(send(u, "path")))
			}
			cb(paths, nil)
		})
	})
}

func (d dialogs) ShowSaveDialog(parent platform.Window, o *platform.SaveDialogOptions, cb func(string, error)) {
	withPool(func() {
		panel := send(class("NSSavePanel"), "savePanel")
		if o.NameFieldLabel != "" {
			send(panel, "setNameFieldLabel:", uintptr(nsString(o.NameFieldLabel)))
		}
		setupSavePanel(panel, o.Title, o.Message, o.ButtonLabel, o.DefaultPath, o.Filters,
			o.ShowHiddenFiles, o.CreateDirectories, o.TreatPackagesAsDirectories)
		runPanel(panel, asWindow(parent), "beginSheetModalForWindow:completionHandler:", func(resp int) {
			if resp != nsModalResponseOK {
				cb("", nil)
				return
			}
			cb(goString(send(send(panel, "URL"), "path")), nil)
		})
	})
}

const nsAlertFirstButtonReturn = 1000

func newAlert(kind, message, detail string, buttons []string, defaultID int, destructive []int) id {
	alert := autorelease(alloc("NSAlert"))
	style := uintptr(1) // NSAlertStyleInformational
	switch kind {
	case "warning":
		style = 0
	case "error":
		style = 2
	}
	send(alert, "setAlertStyle:", style)
	send(alert, "setMessageText:", uintptr(nsString(message)))
	if detail != "" {
		send(alert, "setInformativeText:", uintptr(nsString(detail)))
	}
	for _, b := range buttons {
		send(alert, "addButtonWithTitle:", uintptr(nsString(b)))
	}
	items := arrayItems(send(alert, "buttons"))
	for i, btn := range items {
		key := ""
		if i == defaultID {
			key = "\r"
		}
		send(btn, "setKeyEquivalent:", uintptr(nsString(key)))
	}
	for _, i := range destructive {
		if i >= 0 && i < len(items) && respondsTo(items[i], "setHasDestructiveAction:") {
			send(items[i], "setHasDestructiveAction:", 1)
		}
	}
	return alert
}

// alertKeys handles Escape and Cmd+. because a button has only one key
// equivalent, already used by Return when cancel is the default.
// The monitor is made once, with the first cancelable alert.
var alertKeys id

// alertCancels maps the window of each alert showing to its cancel
// button. Main thread only.
var alertCancels = map[id]id{}

func runAlert(alert id, parent *window, cancelID int, done func(resp int)) {
	win := send(alert, "window")
	if items := arrayItems(send(alert, "buttons")); cancelID >= 0 && cancelID < len(items) {
		alertCancels[win] = items[cancelID]
		watchAlertKeys()
	}
	runPanel(alert, parent, "beginSheetModalForWindow:completionHandler:", func(resp int) {
		delete(alertCancels, win)
		done(resp)
	})
}

func watchAlertKeys() {
	if alertKeys != 0 {
		return
	}
	const mask = 1 << 10 // NSEventMaskKeyDown
	blk := newBlock(func(_ objc.Block, ev id) id {
		win := send(ev, "window")
		btn := alertCancels[win]
		if btn == 0 {
			return ev
		}
		// A key typed while the input method composes in a prompt's field
		// is the input method's, as Escape giving the composition up.
		if field := send(win, "firstResponder"); respondsTo(field, "hasMarkedText") && sendBool(field, "hasMarkedText") {
			return ev
		}
		// Shift may type the period, as on AZERTY, which AppKit's key
		// bindings allow.
		mods, chars := eventMods(ev), goString(send(ev, "charactersIgnoringModifiers"))
		if mods == 0 && chars == "\x1b" || mods&^platform.ModShift == platform.ModSuper && chars == "." {
			send(btn, "performClick:", 0)
			return 0
		}
		return ev
	})
	alertKeys = retain(send(class("NSEvent"), "addLocalMonitorForEventsMatchingMask:handler:", mask, uintptr(blk)))
}

func (d dialogs) ShowMessageBox(parent platform.Window, o *platform.MessageBoxOptions, cb func(platform.MessageBoxResult, error)) {
	withPool(func() {
		message, detail := o.Message, o.Detail
		if message == "" {
			message, detail = o.Title, o.Detail
		}
		alert := newAlert(o.Type, message, detail, o.Buttons, o.DefaultID, o.DestructiveIDs)
		if o.CheckboxLabel != "" {
			send(alert, "setShowsSuppressionButton:", 1)
			btn := send(alert, "suppressionButton")
			send(btn, "setTitle:", uintptr(nsString(o.CheckboxLabel)))
			send(btn, "setState:", boolArg(o.CheckboxChecked))
		}
		runAlert(alert, asWindow(parent), o.CancelID, func(resp int) {
			res := platform.MessageBoxResult{Response: resp - nsAlertFirstButtonReturn}
			if o.CheckboxLabel != "" {
				res.CheckboxChecked = sendInt(send(alert, "suppressionButton"), "state") == 1
			}
			cb(res, nil)
		})
	})
}

// JavaScript panels requested by WKUIDelegate. The completion handler block
// must be called exactly once.

func jsAlert(w *window, message string, handler uintptr) {
	handler = blockCopy(handler)
	withPool(func() {
		alert := newAlert("info", message, "", []string{platform.CurrentTexts().OK}, 0, nil)
		runAlert(alert, w, 0, func(int) {
			callBlock(handler)
			blockRelease(handler)
		})
	})
}

func jsConfirm(w *window, message string, handler uintptr) {
	handler = blockCopy(handler)
	withPool(func() {
		tx := platform.CurrentTexts()
		alert := newAlert("info", message, "", []string{tx.OK, tx.Cancel}, 0, nil)
		runAlert(alert, w, 1, func(resp int) {
			callBlock(handler, boolArg(resp == nsAlertFirstButtonReturn))
			blockRelease(handler)
		})
	})
}

func jsPrompt(w *window, prompt, defaultText string, handler uintptr) {
	handler = blockCopy(handler)
	withPool(func() {
		tx := platform.CurrentTexts()
		alert := newAlert("info", prompt, "", []string{tx.OK, tx.Cancel}, 0, nil)
		field := autorelease(msgInitRect(send(class("NSTextField"), "alloc"), sel("initWithFrame:"), NSRect{Size: NSSize{280, 24}}))
		send(field, "setStringValue:", uintptr(nsString(defaultText)))
		send(alert, "setAccessoryView:", uintptr(field))
		send(send(alert, "window"), "setInitialFirstResponder:", uintptr(field))
		runAlert(alert, w, 1, func(resp int) {
			var result id
			if resp == nsAlertFirstButtonReturn {
				result = send(field, "stringValue")
			}
			callBlock(handler, uintptr(result))
			blockRelease(handler)
		})
	})
}

func jsFileInput(w *window, params id, handler uintptr) {
	handler = blockCopy(handler)
	withPool(func() {
		panel := send(class("NSOpenPanel"), "openPanel")
		send(panel, "setCanChooseFiles:", 1)
		send(panel, "setAllowsMultipleSelection:", boolArg(sendBool(params, "allowsMultipleSelection")))
		if respondsTo(params, "allowsDirectories") {
			send(panel, "setCanChooseDirectories:", boolArg(sendBool(params, "allowsDirectories")))
		}
		runPanel(panel, w, "beginSheetModalForWindow:completionHandler:", func(resp int) {
			var urls id
			if resp == nsModalResponseOK {
				urls = send(panel, "URLs")
			}
			callBlock(handler, uintptr(urls))
			blockRelease(handler)
		})
	})
}
