//go:build linux && (amd64 || arm64)

package linux

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

// Sessions (mygo.Session): a store is a WebKitWebContext of its own, with
// a WebKitWebsiteDataManager whose data and caches are in the store's
// directories, or kept in memory for an ephemeral one. Its web views are
// made with the context; custom schemes, the languages and downloads are
// set up on it as on the default context.

var (
	sessionsOnce                              sync.Once
	webkitWebsiteDataManagerNew               func(k1, v1, k2, v2 *byte, end ptr) ptr
	webkitWebsiteDataManagerNewEphemeral      func() ptr
	webkitWebContextNewWithWebsiteDataManager func(manager ptr) ptr
	webkitWebViewGetType                      func() uintptr
	gObjectNewWebView                         func(t uintptr, k1 *byte, v1 ptr, k2 *byte, v2 ptr, end ptr) ptr
)

func bindSessions() {
	bind(libWebKit, &webkitWebsiteDataManagerNew, "webkit_website_data_manager_new")
	bind(libWebKit, &webkitWebsiteDataManagerNewEphemeral, "webkit_website_data_manager_new_ephemeral")
	bind(libWebKit, &webkitWebContextNewWithWebsiteDataManager, "webkit_web_context_new_with_website_data_manager")
	bind(libWebKit, &webkitWebViewGetType, "webkit_web_view_get_type")
	bind(libGObject, &gObjectNewWebView, "g_object_new")
}

// dataStore is a store of the pages of a session: a web context, and the
// custom schemes registered on it.
type dataStore struct {
	b       *Backend
	ctx     ptr
	schemes map[string]bool
	closed  bool
}

// NewDataStore makes a web context with a website data manager of its
// own: WebKitGTK 2.16 or newer.
func (a appController) NewDataStore(opts platform.DataStoreOptions) (platform.DataStore, error) {
	if err := webKit(); err != nil {
		return nil, err
	}
	sessionsOnce.Do(bindSessions)
	if webkitWebsiteDataManagerNew == nil || webkitWebsiteDataManagerNewEphemeral == nil || webkitWebContextNewWithWebsiteDataManager == nil || webkitWebViewGetType == nil || gObjectNewWebView == nil {
		return nil, fmt.Errorf("sessions need WebKitGTK 2.16 or newer: %w", platform.ErrUnsupported)
	}
	var manager ptr
	if opts.Ephemeral {
		manager = webkitWebsiteDataManagerNewEphemeral()
	} else {
		if opts.DataDir == "" || opts.CacheDir == "" {
			return nil, fmt.Errorf("no directory for the session's data")
		}
		if err := os.MkdirAll(opts.DataDir, 0o700); err != nil {
			return nil, err
		}
		manager = webkitWebsiteDataManagerNew(cs("base-data-directory"), cs(opts.DataDir), cs("base-cache-directory"), cs(opts.CacheDir), 0)
	}
	if manager == 0 {
		return nil, fmt.Errorf("WebKitGTK made no website data manager")
	}
	ctx := webkitWebContextNewWithWebsiteDataManager(manager)
	gObjectUnref(manager) // the context holds it
	s := &dataStore{b: a.b, ctx: ctx, schemes: map[string]bool{}}
	a.b.stores = append(a.b.stores, s)
	a.b.applyWebLanguages()
	connect(ctx, "download-started", cbDownloadStarted, 0)
	if !opts.Ephemeral && cookieManager != nil && cookieSetPersistentStorage != nil {
		// As the default context's (persistCookies): WebKitGTK keeps
		// cookies in memory unless told where to store them.
		const sqlite = 1 // WEBKIT_COOKIE_PERSISTENT_STORAGE_SQLITE
		cookieSetPersistentStorage(cookieManager(ctx), cs(filepath.Join(opts.DataDir, "cookies.sqlite")), sqlite)
	}
	return s, nil
}

// Close drops the backend's reference to the context, which its web
// views keep while they live: an ephemeral one's data goes with the last.
func (s *dataStore) Close() {
	if s.closed {
		return
	}
	s.closed = true
	s.b.stores = slices.DeleteFunc(s.b.stores, func(x *dataStore) bool { return x == s })
	gObjectUnref(s.ctx)
}

// webContext returns the web context of store, the default one for nil.
func webContext(store platform.DataStore) ptr {
	if s, ok := store.(*dataStore); ok {
		return s.ctx
	}
	return webkitWebContextGetDefault()
}

// newWebView makes the web view of w, with the web context of its store.
func (w *window) newWebView() ptr {
	s, ok := w.opts.DataStore.(*dataStore)
	if !ok {
		for _, scheme := range w.opts.Schemes {
			w.b.registerScheme(webkitWebContextGetDefault(), w.b.schemes, scheme)
		}
		return webkitWebViewNewWithUserContentManager(w.ucm)
	}
	for _, scheme := range w.opts.Schemes {
		w.b.registerScheme(s.ctx, s.schemes, scheme)
	}
	return gObjectNewWebView(webkitWebViewGetType(), cs("web-context"), s.ctx, cs("user-content-manager"), w.ucm, 0)
}

// websiteDataTypes returns the WebKitWebsiteDataTypes of types.
func websiteDataTypes(types platform.SiteData) uint32 {
	if types.All() {
		return 1<<14 - 1 // WEBKIT_WEBSITE_DATA_ALL
	}
	var out uint32
	if types&platform.SiteDataCookies != 0 {
		out |= 1 << 8 // COOKIES
	}
	if types&platform.SiteDataLocalStorage != 0 {
		out |= 1<<3 | 1<<4 // SESSION_STORAGE, LOCAL_STORAGE
	}
	if types&platform.SiteDataIndexedDB != 0 {
		out |= 1<<5 | 1<<6 // WEBSQL_DATABASES, INDEXEDDB_DATABASES
	}
	if types&platform.SiteDataCaches != 0 {
		out |= 1<<0 | 1<<1 | 1<<2 | 1<<13 // MEMORY_CACHE, DISK_CACHE, OFFLINE_APPLICATION_CACHE, DOM_CACHE
	}
	if types&platform.SiteDataServiceWorkers != 0 {
		out |= 1 << 12 // SERVICE_WORKER_REGISTRATIONS
	}
	return out
}
