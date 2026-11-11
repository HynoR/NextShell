//go:build windows && (amd64 || arm64)

package windows

import (
	"errors"
	"slices"
	"strconv"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// Sessions (mygo.Session): a store is a WebView2 profile of the app's
// environment, named for the session, or an InPrivate one of a name of its
// own for an ephemeral session, whose data WebView2 keeps in memory until
// the last web view of the profile closes. A web view gets its profile
// from the options of its controller (ICoreWebView2Environment10), and
// cookies come from the cookie manager of a web view of the profile.

// Vtable indices from WebView2.h (SDK 1.0.4191.47).
const (
	env10CreateControllerOptions                = 20 // ICoreWebView2Environment10
	env10CreateControllerWithOptions            = 21
	env10CreateCompositionControllerWithOptions = 22
	ctlOptsPutProfileName                       = 4 // ICoreWebView2ControllerOptions
	ctlOptsPutIsInPrivateModeEnabled            = 6
	profilePutPreferredColorScheme              = 9  // ICoreWebView2Profile
	profile2ClearBrowsingData                   = 10 // ICoreWebView2Profile2
)

var iidICoreWebView2Environment10 = guid("ee0eb9df-6f12-46ce-b53f-3f47b9c928e0")

// errNoProfiles is why a web view of a session cannot be made.
var errNoProfiles = errors.New("mygo: sessions need a newer WebView2 Runtime (profiles, 1.0.1185 or newer)")

// dataStore is the WebView2 profile of a session, and the state of the
// cookie calls of its web views.
type dataStore struct {
	b       *Backend
	profile string
	private bool
	closed  bool
	cookies cookieStore
}

// NewDataStore names the profile of a session: "mygo-" and the session's
// UUID, or for an ephemeral session an InPrivate profile no other store
// of the run has.
func (a appController) NewDataStore(opts platform.DataStoreOptions) (platform.DataStore, error) {
	b := a.b
	s := &dataStore{b: b, profile: "mygo-" + opts.ID, private: opts.Ephemeral}
	if opts.Ephemeral {
		b.privateStores++
		s.profile = "mygo-private-" + strconv.Itoa(b.privateStores)
	}
	s.cookies.data = s
	b.stores = append(b.stores, s)
	return s, nil
}

// Close lets the profile go: the hidden web view of its cookie calls
// closes once none needs it, and an InPrivate profile's data goes with its
// last web view.
func (s *dataStore) Close() {
	if s.closed {
		return
	}
	s.closed = true
	s.b.closeCookieView(&s.cookies)
	s.b.dropStores()
}

// dropStores forgets the closed stores no cookie call or hidden web view
// uses any more.
func (b *Backend) dropStores() {
	b.stores = slices.DeleteFunc(b.stores, func(s *dataStore) bool {
		return s.closed && s.cookies.controller == 0 && !s.cookies.creating && len(s.cookies.requests) == 0
	})
}

// hasProfiles reports whether the environment makes web views of
// profiles other than the default one.
func (b *Backend) hasProfiles() bool {
	env10 := queryInterface(b.env, &iidICoreWebView2Environment10)
	if env10 == 0 {
		return false
	}
	release(env10)
	return true
}

// storeOf returns the profile of store, nil for the default one.
func storeOf(store platform.DataStore) *dataStore {
	s, _ := store.(*dataStore)
	return s
}

// createController asks the environment for the controller of a web view
// of the profile s, nil for the default one, in hwnd, through handler;
// composition asks for a composition controller (an embedded web view's).
// s is a *dataStore, not a platform.DataStore, which a nil *dataStore
// would make a non-nil interface of.
func (b *Backend) createController(s *dataStore, hwnd uintptr, composition bool, handler uintptr) uintptr {
	if s == nil {
		if composition {
			env3 := queryInterface(b.env, &iidICoreWebView2Environment3)
			if env3 == 0 {
				return eNoInterface // a runtime older than visual hosting
			}
			defer release(env3)
			return comCall(env3, env3CreateCompositionController, hwnd, handler)
		}
		return comCall(b.env, envCreateController, hwnd, handler)
	}
	env10 := queryInterface(b.env, &iidICoreWebView2Environment10)
	if env10 == 0 {
		return eNoInterface
	}
	defer release(env10)
	var options uintptr
	if hr := comCall(env10, env10CreateControllerOptions, uintptr(unsafe.Pointer(&options))); failed(hr) || options == 0 {
		return hr
	}
	defer release(options)
	if hr := comCall(options, ctlOptsPutProfileName, uintptr(unsafe.Pointer(u16(s.profile)))); failed(hr) {
		return hr
	}
	if s.private {
		if hr := comCall(options, ctlOptsPutIsInPrivateModeEnabled, 1); failed(hr) {
			return hr
		}
	}
	if composition {
		return comCall(env10, env10CreateCompositionControllerWithOptions, hwnd, options, handler)
	}
	return comCall(env10, env10CreateControllerWithOptions, hwnd, options, handler)
}

// browsingDataKinds returns the COREWEBVIEW2_BROWSING_DATA_KINDS of
// types, but SiteDataAll, which ClearBrowsingDataAll clears.
func browsingDataKinds(types platform.SiteData) uintptr {
	var kinds uintptr
	if types&platform.SiteDataCookies != 0 {
		kinds |= 0x40 // COOKIES
	}
	if types&platform.SiteDataLocalStorage != 0 {
		kinds |= 0x4 // LOCAL_STORAGE
	}
	if types&platform.SiteDataIndexedDB != 0 {
		kinds |= 0x2 | 0x8 // INDEXED_DB, WEB_SQL
	}
	if types&platform.SiteDataCaches != 0 {
		kinds |= 0x10 | 0x100 // CACHE_STORAGE, DISK_CACHE
	}
	if types&platform.SiteDataServiceWorkers != 0 {
		kinds |= 0x8000 // SERVICE_WORKERS
	}
	return kinds
}
