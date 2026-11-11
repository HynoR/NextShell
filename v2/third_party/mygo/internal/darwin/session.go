//go:build darwin

package darwin

import (
	"log"
	"sync"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/internal/platform"
)

// Sessions (mygo.Session): a store is a WKWebsiteDataStore, which the
// configuration of each web view of the session names. window.open hands
// the opener's configuration over, and its store with it.

// dataStore holds a WKWebsiteDataStore, retained until Close.
type dataStore struct {
	store  id
	closed bool
}

var inMemoryOnce sync.Once

// NewDataStore makes the store of a session: a persistent one is
// +dataStoreForIdentifier: of the session's UUID (macOS 14), kept in
// memory until the app quits on older systems, an ephemeral one
// +nonPersistentDataStore.
func (a appController) NewDataStore(opts platform.DataStoreOptions) (platform.DataStore, error) {
	s := &dataStore{}
	withPool(func() {
		cls := class("WKWebsiteDataStore")
		if !opts.Ephemeral && sendBool(cls, "respondsToSelector:", uintptr(sel("dataStoreForIdentifier:"))) {
			uuid := send(send(class("NSUUID"), "alloc"), "initWithUUIDString:", uintptr(nsString(opts.ID)))
			s.store = retain(send(cls, "dataStoreForIdentifier:", uintptr(uuid)))
			release(uuid)
			return
		}
		if !opts.Ephemeral {
			inMemoryOnce.Do(func() {
				log.Print("mygo: sessions keep their data in memory until the app quits before macOS 14")
			})
		}
		s.store = retain(send(cls, "nonPersistentDataStore"))
	})
	return s, nil
}

// Close releases the store, which its web views keep while they live.
func (s *dataStore) Close() {
	if !s.closed {
		s.closed = true
		release(s.store)
	}
}

// websiteDataStore returns the WKWebsiteDataStore of store, the default
// one for nil.
func websiteDataStore(store platform.DataStore) id {
	if s, ok := store.(*dataStore); ok {
		return s.store
	}
	return send(class("WKWebsiteDataStore"), "defaultDataStore")
}

// websiteDataTypes returns the set of WKWebsiteDataTypes of types, those
// of the system's WebKit.
func websiteDataTypes(types platform.SiteData) id {
	if types.All() {
		return send(class("WKWebsiteDataStore"), "allWebsiteDataTypes")
	}
	set := send(class("NSMutableSet"), "set")
	add := func(names ...string) {
		for _, name := range names {
			if t := constString(libWebKit, name); t != 0 {
				send(set, "addObject:", uintptr(t))
			}
		}
	}
	if types&platform.SiteDataCookies != 0 {
		add("WKWebsiteDataTypeCookies")
	}
	if types&platform.SiteDataLocalStorage != 0 {
		add("WKWebsiteDataTypeLocalStorage", "WKWebsiteDataTypeSessionStorage")
	}
	if types&platform.SiteDataIndexedDB != 0 {
		add("WKWebsiteDataTypeIndexedDBDatabases", "WKWebsiteDataTypeWebSQLDatabases")
	}
	if types&platform.SiteDataCaches != 0 {
		add("WKWebsiteDataTypeDiskCache", "WKWebsiteDataTypeMemoryCache", "WKWebsiteDataTypeOfflineWebApplicationCache", "WKWebsiteDataTypeFetchCache")
	}
	if types&platform.SiteDataServiceWorkers != 0 {
		add("WKWebsiteDataTypeServiceWorkerRegistrations")
	}
	return set
}

// removeData removes the data of types from store, whatever its age.
func (a appController) removeData(store platform.DataStore, types platform.SiteData, done func(error)) {
	withPool(func() {
		blk := newBlock(func(_ objc.Block) { a.b.runOnMain(func() { done(nil) }) })
		send(websiteDataStore(store), "removeDataOfTypes:modifiedSince:completionHandler:", uintptr(websiteDataTypes(types)), uintptr(send(class("NSDate"), "distantPast")), uintptr(blk))
		blk.Release()
	})
}
