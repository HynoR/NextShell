//go:build linux && (amd64 || arm64)

package linux

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

var (
	filterStoreNew                               func(path *byte) ptr
	filterStoreSave                              func(store ptr, identifier *byte, source, cancellable, cb, data ptr)
	filterStoreLoad, filterStoreRemove           func(store ptr, identifier *byte, cancellable, cb, data ptr)
	filterStoreSaveFinish, filterStoreLoadFinish func(store, result ptr, err *ptr) ptr
	filterStoreRemoveFinish                      func(store, result ptr, err *ptr) int32
	filterStoreFetchIdentifiers                  func(store, cancellable, cb, data ptr)
	filterStoreFetchIdentifiersFinish            func(store, result ptr) ptr
	userContentManagerAddFilter                  func(ucm, filter ptr)
	userContentManagerRemoveAllFilters           func(ucm ptr)
	gBytesNew                                    func(data *byte, size uintptr) ptr
	gBytesUnref                                  func(bytes ptr)
	contentFiltersOnce                           sync.Once
	contentFilterStore                           ptr
	errContentFilters                            error
)

// contentRules is the code of content rules, which LinkContentRules
// links: the backend's methods are linked into every app, and only call
// it.
var contentRules struct {
	compile func(id, identifier, rules string, done func(any, error))
	set     func(w *window, lists []any)
}

// LinkContentRules links the code of content rules, when the app first
// compiles rules.
func LinkContentRules() {
	contentRules.compile, contentRules.set = compileContentRules, setContentRules
}

func (a appController) CompileContentRules(id, identifier, rules string, done func(any, error)) {
	if contentRules.compile == nil {
		done(nil, platform.ErrUnsupported)
		return
	}
	contentRules.compile(id, identifier, rules, done)
}

func (w *window) SetContentRules(lists []any) {
	if contentRules.set != nil {
		contentRules.set(w, lists)
	}
}

// contentFilters binds WebKitGTK's content filters, from 2.24 on, and
// opens the store that keeps compiled filters in the app's user data
// directory, the first time the app compiles rules.
func contentFilters() (ptr, error) {
	if err := webKit(); err != nil {
		return 0, err
	}
	contentFiltersOnce.Do(func() {
		w := libWebKit
		if !bind(w, &filterStoreNew, "webkit_user_content_filter_store_new") ||
			!bind(w, &filterStoreSave, "webkit_user_content_filter_store_save") ||
			!bind(w, &filterStoreSaveFinish, "webkit_user_content_filter_store_save_finish") ||
			!bind(w, &filterStoreLoad, "webkit_user_content_filter_store_load") ||
			!bind(w, &filterStoreLoadFinish, "webkit_user_content_filter_store_load_finish") ||
			!bind(w, &filterStoreRemove, "webkit_user_content_filter_store_remove") ||
			!bind(w, &filterStoreRemoveFinish, "webkit_user_content_filter_store_remove_finish") ||
			!bind(w, &filterStoreFetchIdentifiers, "webkit_user_content_filter_store_fetch_identifiers") ||
			!bind(w, &filterStoreFetchIdentifiersFinish, "webkit_user_content_filter_store_fetch_identifiers_finish") ||
			!bind(w, &userContentManagerAddFilter, "webkit_user_content_manager_add_filter") ||
			!bind(w, &userContentManagerRemoveAllFilters, "webkit_user_content_manager_remove_all_filters") ||
			!bind(libGLib, &gBytesNew, "g_bytes_new") || !bind(libGLib, &gBytesUnref, "g_bytes_unref") {
			errContentFilters = fmt.Errorf("content rules need WebKitGTK 2.24 or newer: %w", platform.ErrUnsupported)
			return
		}
		dir, err := os.UserConfigDir()
		if err != nil {
			errContentFilters = err
			return
		}
		dir = filepath.Join(dir, theBackend.name, "content-rules")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			errContentFilters = err
			return
		}
		contentFilterStore = filterStoreNew(cs(dir))
	})
	return contentFilterStore, errContentFilters
}

// compileContentRules loads the filter the store keeps as identifier,
// else compiles rules into it and removes id's earlier filters. The
// handle is the WebKitUserContentFilter, whose reference it keeps.
func compileContentRules(id, identifier, rules string, done func(any, error)) {
	store, err := contentFilters()
	if err != nil {
		done(nil, err)
		return
	}
	save := func() {
		source := gBytesNew(unsafe.StringData(rules), uintptr(len(rules)))
		defer gBytesUnref(source)
		request := pending.add(func(_, result ptr) {
			var ge ptr
			filter := filterStoreSaveFinish(store, result, &ge)
			if err := gErr(ge); err != nil || filter == 0 {
				if err == nil {
					err = fmt.Errorf("WebKitGTK made no filter")
				}
				done(nil, err)
				return
			}
			removeStaleContentFilters(store, id, identifier)
			done(filter, nil)
		})
		// GBytes copies the rules.
		filterStoreSave(store, cs(identifier), source, 0, cbAsyncReady, request)
	}
	request := pending.add(func(_, result ptr) {
		var ge ptr
		filter := filterStoreLoadFinish(store, result, &ge)
		if gErr(ge) != nil || filter == 0 {
			save()
			return
		}
		done(filter, nil)
	})
	filterStoreLoad(store, cs(identifier), 0, cbAsyncReady, request)
}

// removeStaleContentFilters removes the filters the store keeps for id
// but current.
func removeStaleContentFilters(store ptr, id, current string) {
	request := pending.add(func(_, result ptr) {
		list := filterStoreFetchIdentifiersFinish(store, result)
		if list == 0 {
			return
		}
		for i := uintptr(0); ; i++ {
			s := field[ptr](list, i*8)
			if s == 0 {
				break
			}
			if name := goStr(s); platform.StaleContentRules(id, current, name) {
				removed := pending.add(func(_, result ptr) {
					var ge ptr
					filterStoreRemoveFinish(store, result, &ge)
					gErr(ge)
				})
				filterStoreRemove(store, cs(name), 0, cbAsyncReady, removed)
			}
		}
		gStrfreev(list)
	})
	filterStoreFetchIdentifiers(store, 0, cbAsyncReady, request)
}

// setContentRules replaces the filters of the window's content manager.
func setContentRules(w *window, lists []any) {
	if w.ucm == 0 || w.closed || userContentManagerRemoveAllFilters == nil {
		return
	}
	userContentManagerRemoveAllFilters(w.ucm)
	for _, l := range lists {
		if filter, ok := l.(ptr); ok {
			userContentManagerAddFilter(w.ucm, filter)
		}
	}
}
