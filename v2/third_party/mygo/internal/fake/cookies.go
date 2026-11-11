package fake

import (
	"net/url"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// DataStore is a website data store NewDataStore made, with cookies of
// its own.
type DataStore struct {
	b    *Backend
	Opts platform.DataStoreOptions
	// Closed tells that Close was called; Cleared lists the types of
	// ClearBrowsingData calls.
	Closed  bool
	Cleared []platform.SiteData
	cookies map[platform.CookieKey]platform.Cookie
}

// Close marks the store closed: its cookies go.
func (s *DataStore) Close() {
	s.b.mu.Lock()
	s.Closed = true
	s.cookies = nil
	s.b.mu.Unlock()
}

// NewDataStore makes a store, which DataStores lists.
func (a app) NewDataStore(opts platform.DataStoreOptions) (platform.DataStore, error) {
	s := &DataStore{b: a.b, Opts: opts}
	a.b.mu.Lock()
	a.b.DataStores = append(a.b.DataStores, s)
	a.b.mu.Unlock()
	return s, nil
}

// ClearBrowsingData counts the calls for the default store, Cleared, and
// lists their types for the others; cookies go with SiteDataCookies.
func (a app) ClearBrowsingData(store platform.DataStore, types platform.SiteData, done func(error)) {
	a.b.mu.Lock()
	if s, ok := store.(*DataStore); ok {
		s.Cleared = append(s.Cleared, types)
	} else {
		a.b.Cleared++
	}
	if types&platform.SiteDataCookies != 0 {
		clear(*a.cookies(store))
	}
	a.b.mu.Unlock()
	done(nil)
}

// cookies returns the cookies of store, nil for the default one. The
// lock must be held.
func (a app) cookies(store platform.DataStore) *map[platform.CookieKey]platform.Cookie {
	m := &a.b.cookies
	if s, ok := store.(*DataStore); ok {
		m = &s.cookies
	}
	if *m == nil {
		*m = map[platform.CookieKey]platform.Cookie{}
	}
	return m
}

// ListCookies returns the cookies that have not expired, those a request
// to rawURL sends unless it is empty.
func (a app) ListCookies(store platform.DataStore, rawURL string, done func([]platform.Cookie, error)) {
	var u *url.URL
	if rawURL != "" {
		var err error
		if u, err = url.Parse(rawURL); err != nil {
			done(nil, err)
			return
		}
	}
	a.b.mu.Lock()
	now := time.Now()
	result := []platform.Cookie{}
	for _, c := range *a.cookies(store) {
		if (c.Expires.IsZero() || c.Expires.After(now)) && (u == nil || platform.CookieMatchesURL(c, u)) {
			result = append(result, c)
		}
	}
	a.b.mu.Unlock()
	done(result, nil)
}

func (a app) SetCookie(store platform.DataStore, c platform.Cookie, done func(error)) {
	a.b.mu.Lock()
	(*a.cookies(store))[platform.CookieKey{Name: c.Name, Domain: c.Domain, Path: c.Path}] = c
	a.b.mu.Unlock()
	done(nil)
}

func (a app) DeleteCookie(store platform.DataStore, key platform.CookieKey, done func(error)) {
	a.b.mu.Lock()
	delete(*a.cookies(store), key)
	a.b.mu.Unlock()
	done(nil)
}

func (a app) ClearCookies(store platform.DataStore, done func(error)) {
	a.b.mu.Lock()
	clear(*a.cookies(store))
	a.b.CookiesCleared++
	a.b.mu.Unlock()
	done(nil)
}
