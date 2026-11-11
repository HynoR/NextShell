//go:build windows && (amd64 || arm64)

package windows

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo/internal/webview2"
)

// Vtable indices of the WebView2 interfaces used here, from WebView2.h
// (SDK 1.0.4191.47). Each interface extends the previous version, so the
// indices stay valid for newer runtimes.
const (
	// ICoreWebView2Environment
	envCreateController          = 3
	envCreateWebResourceResponse = 4

	// ICoreWebView2Controller, ICoreWebView2Controller2
	ctlPutIsVisible                      = 4
	ctlPutBounds                         = 6
	ctlGetZoomFactor                     = 7
	ctlPutZoomFactor                     = 8
	ctlMoveFocus                         = 12
	ctlAddAcceleratorKeyPressed          = 19
	ctlNotifyParentWindowPositionChanged = 23
	ctlClose                             = 24
	ctlGetCoreWebView2                   = 25
	ctl2PutDefaultBackgroundColor        = 27

	// ICoreWebView2
	wvGetSettings                            = 3
	wvGetSource                              = 4
	wvNavigate                               = 5
	wvNavigateToString                       = 6
	wvAddNavigationStarting                  = 7
	wvAddContentLoading                      = 9
	wvAddPermissionRequested                 = 23
	wvAddNavigationCompleted                 = 15
	wvAddProcessFailed                       = 25
	wvAddScriptToExecuteOnDocumentCreated    = 27
	wvRemoveScriptToExecuteOnDocumentCreated = 28
	wvExecuteScript                          = 29
	wvCapturePreview                         = 30
	wvReload                                 = 31
	wvAddWebMessageReceived                  = 34
	wvCallDevToolsProtocolMethod             = 36
	wvGetBrowserProcessId                    = 37
	wvGetCanGoBack                           = 38
	wvGetCanGoForward                        = 39
	wvGoBack                                 = 40
	wvGoForward                              = 41
	wvStop                                   = 43
	wvAddNewWindowRequested                  = 44
	wvAddDocumentTitleChanged                = 46
	wvGetDocumentTitle                       = 48
	wvOpenDevToolsWindow                     = 51
	wvAddWebResourceRequested                = 55
	wvAddWebResourceRequestedFilter          = 57
	wvRemoveWebResourceRequestedFilter       = 58
	wvAddWindowCloseRequested                = 59

	// ICoreWebView2Settings, ICoreWebView2Settings2
	setPutIsScriptEnabled                = 4
	setPutIsWebMessageEnabled            = 6
	setPutAreDefaultScriptDialogsEnabled = 8
	setPutIsStatusBarEnabled             = 10
	setPutAreDevToolsEnabled             = 12
	setPutAreDefaultContextMenusEnabled  = 14
	setPutIsZoomControlEnabled           = 18
	set2GetUserAgent                     = 21
	set2PutUserAgent                     = 22

	// ICoreWebView2Settings3
	set3GetAreBrowserAcceleratorKeysEnabled = 23
	set3PutAreBrowserAcceleratorKeysEnabled = 24

	// ICoreWebView2Settings5, ICoreWebView2Settings6
	set5GetIsPinchZoomEnabled       = 29
	set5PutIsPinchZoomEnabled       = 30
	set6GetIsSwipeNavigationEnabled = 31
	set6PutIsSwipeNavigationEnabled = 32

	// Event arguments and helpers.
	navStartingGetURI             = 3
	navStartingGetIsUserInitiated = 4
	navStartingPutCancel          = 8
	navStarting3GetNavigationKind = 12
	navCompletedGetIsSuccess      = 3
	navCompletedGetWebErrorStatus = 4
	msgTryGetWebMessageAsString   = 5
	msg2GetAdditionalObjects      = 6
	objectsGetCount               = 3
	objectsGetValueAtIndex        = 4
	fileGetPath                   = 3
	newWinGetURI                  = 3
	newWinPutNewWindow            = 4
	newWinPutHandled              = 6
	newWinGetIsUserInitiated      = 8 // after get_NewWindow (5), put_Handled (6), get_Handled (7)
	newWinGetDeferral             = 9
	newWinGetWindowFeatures       = 10
	featGetHasPosition            = 3
	featGetHasSize                = 4
	featGetLeft                   = 5
	featGetTop                    = 6
	featGetHeight                 = 7
	featGetWidth                  = 8
	resReqGetRequest              = 3
	resReqPutResponse             = 5
	resReqGetDeferral             = 6
	resReqGetResourceContext      = 7
	reqGetURI                     = 3
	reqGetMethod                  = 5
	reqGetContent                 = 7
	reqGetHeaders                 = 9
	headersRemoveHeader           = 7
	headersGetIterator            = 8
	iterGetCurrentHeader          = 3
	iterGetHasCurrentHeader       = 4
	iterMoveNext                  = 5
	deferralComplete              = 3
	procFailedGetKind             = 3
	wv4AddDownloadStarting        = 75  // ICoreWebView2_4
	wv13GetProfile                = 105 // ICoreWebView2_13
	profile2ClearBrowsingDataAll  = 12  // ICoreWebView2Profile2
	dlStartGetOperation           = 3
	dlStartPutCancel              = 5
	dlStartGetResultFilePath      = 6
	dlStartPutResultFilePath      = 7
	dlStartPutHandled             = 9
	dlOpAddStateChanged           = 7
	dlOpGetURI                    = 9
	dlOpGetState                  = 16
	dlOpGetInterruptReason        = 17
	permGetURI                    = 3
	permGetKind                   = 4
	permPutState                  = 7
	accelGetKeyEventKind          = 3
	accelGetVirtualKey            = 4
	accelPutHandled               = 8
)

// ICoreWebView2_8, whose methods follow ICoreWebView2_7's PrintToPdf (80)
// in WebView2.h: add_IsMutedChanged (81), remove_IsMutedChanged (82),
// get_IsMuted (83), put_IsMuted (84), add_IsDocumentPlayingAudioChanged
// (85), remove_IsDocumentPlayingAudioChanged (86) and
// get_IsDocumentPlayingAudio (87).
const (
	wv8AddIsMutedChanged                = 81
	wv8GetIsMuted                       = 83
	wv8PutIsMuted                       = 84
	wv8AddIsDocumentPlayingAudioChanged = 85
	wv8GetIsDocumentPlayingAudio        = 87
)

var (
	iidICoreWebView2Controller2                  = guid("c979903e-d4ca-4228-92eb-47ee3fa96eab")
	iidICoreWebView2Settings2                    = guid("ee9a0f68-f46c-4e32-ac23-ef8cac224d2a")
	iidICoreWebView2Settings3                    = guid("fdb5ab74-af33-4854-84f0-0a631deb5eba")
	iidICoreWebView2Settings5                    = guid("183e7052-1d03-43a0-ab99-98e043b66b39")
	iidICoreWebView2Settings6                    = guid("11cb3acd-9bc8-43b8-83bf-f40753714f87")
	iidICoreWebView2WebMessageReceivedEventArgs2 = guid("06fc7ab7-c90c-4297-9389-33ca01cf6d5e")
	iidICoreWebView2NavigationStartingEventArgs3 = guid("ddffe494-4942-4bd2-ab73-35b8ff40e19f")
	iidICoreWebView2File                         = guid("f2c19559-6bc1-4583-a757-90021be9afec")
	iidICoreWebView2_4                           = guid("20d02d59-6df2-42dc-bd06-f98a694b1302")
	iidICoreWebView2_8                           = guid("e9632730-6e1e-43ab-b7b8-7b2c9e62e094")
	iidICoreWebView2_13                          = guid("f75f09a8-667e-4983-88d6-c8773f315e84")
	iidICoreWebView2Profile2                     = guid("fa740d4b-5eae-4344-a8ad-74be31925397")
	iidICoreWebView2Environment8                 = guid("d6eb91dd-c3d2-45e5-bd29-6dc2bc4de9cf")
)

// ICoreWebView2Environment8's GetProcessInfos follows
// add_ProcessInfosChanged (16) and remove_ProcessInfosChanged (17); the
// collection it returns has get_Count and GetValueAtIndex, each
// ICoreWebView2ProcessInfo get_ProcessId and get_Kind.
const (
	env8GetProcessInfos      = 18
	procInfosGetCount        = 3
	procInfosGetValueAtIndex = 4
	procInfoGetProcessId     = 3
	procInfoGetKind          = 4

	processKindGPU = 4 // COREWEBVIEW2_PROCESS_KIND_GPU
)

const (
	hkeyCurrentUser  = 0x80000001
	hkeyLocalMachine = 0x80000002
	keyQueryValue    = 0x0001
)

// unthrottledArguments are the browser's arguments that keep the pages of
// hidden, minimized and covered windows running at full speed
// (PageOptions.BackgroundThrottling): timers unthrottled, renderers at
// the priority of visible ones, and covered windows not taken for hidden.
const unthrottledArguments = "--disable-background-timer-throttling --disable-renderer-backgrounding --disable-backgrounding-occluded-windows"

// autoplayArguments are the browser's arguments that let pages play
// audible media without a user gesture (PageOptions.Autoplay).
const autoplayArguments = "--autoplay-policy=no-user-gesture-required"

// browserArguments are the browser's additional arguments for a first page
// that asks not to be throttled, or to play without a user gesture, or
// both; "" for neither.
func browserArguments(unthrottled, autoplay bool) string {
	var args []string
	if unthrottled {
		args = append(args, unthrottledArguments)
	}
	if autoplay {
		args = append(args, autoplayArguments)
	}
	return strings.Join(args, " ")
}

// createEnvironment starts creating the WebView2 environment, in language,
// the app's (envOptions), or the system's for "", with the browser's
// additional arguments args; done runs on the main thread with it, and
// whether it has args, or with an error. An official loader next to the
// executable reads WebView2's environment variables itself. Without one,
// the runtime's client library does not, so WEBVIEW2_USER_DATA_FOLDER
// replaces the user data folder and WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS
// are appended to args. A runtime that fails to create one with the
// options is asked for one without them.
func createEnvironment(userData, language, args string, done func(env uintptr, withArgs bool, err error)) error {
	// An official loader next to the executable wins, and reads WebView2's
	// environment variables itself; the runtime's client library does not.
	loader := loaderProc()
	if loader == nil {
		var extra string
		userData, extra = loaderVariables(os.Getenv, executableDir, userData)
		switch {
		case args == "":
			args = extra
		case extra != "":
			args += " " + extra
		}
	}
	options := language != "" || args != ""
	without := func(env uintptr, err error) { done(env, false, err) }
	err := createEnvironmentWith(loader, userData, language, args, func(env uintptr, err error) {
		if err != nil && options {
			// A runtime that takes no options of ours: one without them,
			// in the language of the system.
			log.Printf("%v; creating it again without its language and arguments", err)
			if err := createEnvironmentWith(loader, userData, "", "", without); err != nil {
				without(0, err)
			}
			return
		}
		done(env, args != "", err)
	})
	if err != nil && options {
		return createEnvironmentWith(loader, userData, "", "", without)
	}
	return err
}

// createEnvironmentWith creates the environment as createEnvironment does,
// through loader, or the runtime's client library for nil, with the options
// of language and args, or none when both are "".
func createEnvironmentWith(loader *syscall.LazyProc, userData, language, args string, done func(env uintptr, err error)) error {
	completed := func(hr, env uintptr) {
		if failed(hr) || env == 0 {
			done(0, hresultError("creating the WebView2 environment", hr))
			return
		}
		addRef(env)
		done(env, nil)
	}
	dataDir := u16(userData)
	var options uintptr
	if language != "" || args != "" {
		options = newEnvOptions(language, args)
		defer release(options) // the environment takes its own reference
	}

	// An official loader next to the executable wins.
	if loader != nil {
		hr := withHandler(completed, func(h uintptr) uintptr {
			r, _, _ := loader.Call(0, uintptr(unsafe.Pointer(dataDir)), options, h)
			return r
		})
		if failed(hr) {
			return hresultError("CreateCoreWebView2EnvironmentWithOptions", hr)
		}
		return nil
	}

	// Otherwise the runtime's client library, as the loader does: a fixed
	// version runtime named by WEBVIEW2_BROWSER_EXECUTABLE_FOLDER, else the
	// Evergreen runtime.
	dir, fixed, err := webview2.Find()
	if err != nil {
		return fmt.Errorf("mygo: %w", err)
	}
	client, runtimeType := webview2.ClientDLL(dir), uintptr(0)
	if fixed {
		runtimeType = 1
	}
	dll := syscall.NewLazyDLL(client)
	proc := dll.NewProc("CreateWebViewEnvironmentWithOptionsInternal")
	if err := proc.Find(); err != nil {
		return fmt.Errorf("mygo: unsupported WebView2 Runtime at %s: %w", client, err)
	}
	hr := withHandler(completed, func(h uintptr) uintptr {
		r, _, _ := proc.Call(1, runtimeType, uintptr(unsafe.Pointer(dataDir)), options, h)
		return r
	})
	if failed(hr) {
		return hresultError("creating the WebView2 environment", hr)
	}
	return nil
}

// loaderVariables returns the user data folder and the browser arguments
// that Microsoft's loader takes from WebView2's environment variables and
// the runtime's client library does not, so that MyGo applies them when it
// loads the runtime itself: WEBVIEW2_USER_DATA_FOLDER replaces userData,
// relative to the directory exeDir returns, and
// WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS are appended to the arguments of the
// options, after the browser's own (browserArguments).
func loaderVariables(getenv func(string) string, exeDir func() string, userData string) (dir, args string) {
	dir = getenv("WEBVIEW2_USER_DATA_FOLDER")
	switch {
	case dir == "":
		dir = userData
	case !filepath.IsAbs(dir):
		dir = filepath.Join(exeDir(), dir)
	}
	return dir, getenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS")
}

// executableDir returns the directory of the executable, which a relative
// WEBVIEW2_USER_DATA_FOLDER is relative to, or "" when it is unknown.
func executableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// loaderProc returns the official WebView2 loader next to the executable and
// its CreateCoreWebView2EnvironmentWithOptions, or nil when there is no
// loader to use. createEnvironment calls what it returns, and
// WebViewAvailable asks for it, so the two cannot disagree: a DLL of that
// name with no such export is no loader, and a runtime is looked for
// instead.
func loaderProc() *syscall.LazyProc {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	path := filepath.Join(filepath.Dir(exe), "WebView2Loader.dll")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	proc := syscall.NewLazyDLL(path).NewProc("CreateCoreWebView2EnvironmentWithOptions")
	if proc.Find() != nil {
		return nil
	}
	return proc
}

// WebViewAvailable reports whether a window showing a web page can have
// one: the WebView2 Runtime is installed, or a loader next to the
// executable loads a fixed version runtime or the installed one itself. It
// starts nothing; once a window showing a page created the environment, it
// says whether that worked. It reads the registry and the disk only, so any
// goroutine may call it, before Init too.
func (b *Backend) WebViewAvailable() bool {
	switch b.envResult.Load() {
	case 1:
		return true
	case 2:
		return false
	}
	if loaderProc() != nil {
		return true
	}
	_, _, err := webview2.Find()
	return err == nil
}

// regDWORD reads a DWORD value, reporting whether it exists.
func regDWORD(root uintptr, path, name string) (uint32, bool) {
	var key uintptr
	if r, _, _ := procRegOpenKeyExW.Call(root, uintptr(unsafe.Pointer(u16(path))), 0, keyQueryValue, uintptr(unsafe.Pointer(&key))); r != 0 {
		return 0, false
	}
	defer procRegCloseKey.Call(key)
	var typ, v uint32
	size := uint32(4)
	if r, _, _ := procRegQueryValueExW.Call(key, uintptr(unsafe.Pointer(u16(name))), 0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&size))); r != 0 {
		return 0, false
	}
	return v, true
}
