package mygo

import (
	"strings"

	"github.com/egoist/mygo/internal/platform"
)

// DialogModule shows native dialogs. Its methods block until the dialog is
// dismissed; they can be called from any goroutine.
type DialogModule struct{}

// Dialog shows native file and message dialogs.
var Dialog DialogModule

// FileFilter limits the files a file dialog shows, e.g.
// {Name: "Images", Extensions: []string{"png", "jpg"}}, or by type, as
// HTML's accept: {Name: "Images", MIMETypes: []string{"image/*"}} takes
// every picture the system knows.
type FileFilter struct {
	Name string
	// Extensions are file name extensions, with or without the dot, or
	// "*" for every file.
	Extensions []string
	// MIMETypes are media types, as "image/png", or all of one kind, as
	// "image/*", which the system's picker takes as its own: types
	// (UTType) on macOS 11 and later, which knows image/*, audio/*,
	// video/*, text/* and font/* (earlier versions only those kinds), GTK's
	// MIME patterns, and on Windows the extensions the registry gives the
	// type, or of the kind (its PerceivedType too). The filter takes the
	// files of its extensions and of its types.
	MIMETypes []string
}

// OpenDialogOptions configures Dialog.Open.
type OpenDialogOptions struct {
	// Parent attaches the dialog to a window (a sheet on macOS).
	Parent      *Window
	Title       string
	DefaultPath string
	// ButtonLabel replaces the label of the confirm button.
	ButtonLabel string
	// Message is shown above the file list (macOS).
	Message string
	Filters []FileFilter
	// Directory selects directories instead of files.
	Directory bool
	// Multiple allows selecting several entries.
	Multiple        bool
	ShowHiddenFiles bool
	// CreateDirectories shows a "New Folder" button (macOS).
	CreateDirectories bool
	// TreatPackagesAsDirectories lets the user browse into packages such as
	// .app bundles (macOS).
	TreatPackagesAsDirectories bool
}

// SaveDialogOptions configures Dialog.Save.
type SaveDialogOptions struct {
	Parent      *Window
	Title       string
	DefaultPath string
	ButtonLabel string
	Message     string
	// NameFieldLabel replaces the label in front of the file name field
	// (macOS).
	NameFieldLabel             string
	Filters                    []FileFilter
	ShowHiddenFiles            bool
	CreateDirectories          bool
	TreatPackagesAsDirectories bool
}

// MessageType selects the icon of a message dialog.
type MessageType string

// Message dialog types.
const (
	MessageNone     MessageType = "none"
	MessageInfo     MessageType = "info"
	MessageWarning  MessageType = "warning"
	MessageError    MessageType = "error"
	MessageQuestion MessageType = "question"
)

// MessageOptions configures Dialog.Message.
type MessageOptions struct {
	Parent  *Window
	Type    MessageType
	Title   string
	Message string
	// Detail is shown below Message in a smaller font.
	Detail string
	// Buttons defaults to a single "OK" button.
	Buttons []string
	// DefaultButton is the index of the button activated with Enter, or -1
	// for none (macOS only). Windows uses the first button for -1;
	// GTK gives Enter to the focused control.
	DefaultButton int
	// CancelButton is the index of the button activated with Escape (and
	// Cmd+. on macOS), also when it is the default button. When zero, the
	// first button labeled "Cancel" or "No" is used, else the first button,
	// even a destructive one. -1 is none: Escape then does nothing on
	// macOS, and dismissing the dialog reports Button -1 on Linux, in
	// Windows task dialogs and in browsers' confirm dialogs.
	CancelButton int
	// DestructiveButtons are the indexes of buttons whose actions delete
	// data or can't be undone. macOS 11 and later and Linux set them apart;
	// macOS may keep Enter from activating them, even as the default.
	DestructiveButtons []int
	CheckboxLabel      string
	CheckboxChecked    bool
}

// MessageResult is the outcome of Dialog.Message.
type MessageResult struct {
	// Button is the index of the clicked button.
	Button int
	// CheckboxChecked is the final state of the checkbox.
	CheckboxChecked bool
}

func (w *Window) nativeOrNil() platform.Window {
	if w == nil {
		return nil
	}
	return w.native
}

func toFilters(fs []FileFilter) []platform.FileFilter {
	out := make([]platform.FileFilter, len(fs))
	for i, f := range fs {
		out[i] = platform.FileFilter(f)
	}
	return out
}

// Open shows a dialog to pick files or directories and returns the chosen
// paths, or nil when canceled.
func (DialogModule) Open(opts OpenDialogOptions) ([]string, error) {
	needsApp("Dialog.Open")
	type result struct {
		paths []string
		err   error
	}
	ch := make(chan result, 1)
	popts := &platform.OpenDialogOptions{
		Title:                      opts.Title,
		DefaultPath:                opts.DefaultPath,
		ButtonLabel:                opts.ButtonLabel,
		Message:                    opts.Message,
		Filters:                    toFilters(opts.Filters),
		OpenFiles:                  !opts.Directory,
		OpenDirectories:            opts.Directory,
		Multiple:                   opts.Multiple,
		ShowHiddenFiles:            opts.ShowHiddenFiles,
		CreateDirectories:          opts.CreateDirectories,
		TreatPackagesAsDirectories: opts.TreatPackagesAsDirectories,
	}
	if !postMain(func() {
		backend().Dialogs().ShowOpenDialog(opts.Parent.nativeOrNil(), popts, func(paths []string, err error) {
			deliver(ch, result{paths, err})
		})
	}) {
		return nil, errLoopStopped
	}
	r := await(ch)
	return r.paths, r.err
}

// Save shows a dialog to choose where to save a file and returns the path,
// or "" when canceled.
func (DialogModule) Save(opts SaveDialogOptions) (string, error) {
	needsApp("Dialog.Save")
	type result struct {
		path string
		err  error
	}
	ch := make(chan result, 1)
	popts := &platform.SaveDialogOptions{
		Title:                      opts.Title,
		DefaultPath:                opts.DefaultPath,
		ButtonLabel:                opts.ButtonLabel,
		Message:                    opts.Message,
		NameFieldLabel:             opts.NameFieldLabel,
		Filters:                    toFilters(opts.Filters),
		ShowHiddenFiles:            opts.ShowHiddenFiles,
		CreateDirectories:          opts.CreateDirectories,
		TreatPackagesAsDirectories: opts.TreatPackagesAsDirectories,
	}
	if !postMain(func() {
		backend().Dialogs().ShowSaveDialog(opts.Parent.nativeOrNil(), popts, func(path string, err error) {
			deliver(ch, result{path, err})
		})
	}) {
		return "", errLoopStopped
	}
	r := await(ch)
	return r.path, r.err
}

// Message shows a message dialog and reports which button was clicked.
func (DialogModule) Message(opts MessageOptions) (MessageResult, error) {
	needsApp("Dialog.Message")
	type result struct {
		res MessageResult
		err error
	}
	buttons := opts.Buttons
	if len(buttons) == 0 {
		buttons = []string{menuMessages().Ok()}
	}
	cancel := opts.CancelButton
	if cancel == 0 {
		for i, b := range buttons {
			if l := strings.ToLower(b); l == "cancel" || l == "no" || b == menuMessages().Cancel() {
				cancel = i
				break
			}
		}
	}
	popts := &platform.MessageBoxOptions{
		Type:            string(opts.Type),
		Title:           opts.Title,
		Message:         opts.Message,
		Detail:          opts.Detail,
		Buttons:         buttons,
		DefaultID:       opts.DefaultButton,
		CancelID:        cancel,
		DestructiveIDs:  opts.DestructiveButtons,
		CheckboxLabel:   opts.CheckboxLabel,
		CheckboxChecked: opts.CheckboxChecked,
	}
	ch := make(chan result, 1)
	if !postMain(func() {
		backend().Dialogs().ShowMessageBox(opts.Parent.nativeOrNil(), popts, func(r platform.MessageBoxResult, err error) {
			button := r.Response
			if button < 0 || button >= len(buttons) {
				button = cancel // dismissed without a button, e.g. when quitting
			}
			deliver(ch, result{MessageResult{Button: button, CheckboxChecked: r.CheckboxChecked}, err})
		})
	}) {
		return MessageResult{}, errLoopStopped
	}
	r := await(ch)
	return r.res, r.err
}

// Error shows a modal error dialog.
func (d DialogModule) Error(title, content string) {
	needsApp("Dialog.Error")
	_, _ = d.Message(MessageOptions{Type: MessageError, Message: title, Detail: content})
}
