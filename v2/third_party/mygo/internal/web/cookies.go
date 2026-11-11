//go:build js

package web

import "github.com/egoist/mygo/internal/platform"

func (app) ListCookies(_ platform.DataStore, _ string, done func([]platform.Cookie, error)) {
	done(nil, platform.ErrUnsupported)
}
func (app) SetCookie(_ platform.DataStore, _ platform.Cookie, done func(error)) {
	done(platform.ErrUnsupported)
}
func (app) DeleteCookie(_ platform.DataStore, _ platform.CookieKey, done func(error)) {
	done(platform.ErrUnsupported)
}
func (app) ClearCookies(_ platform.DataStore, done func(error)) { done(platform.ErrUnsupported) }
func (app) CompileContentRules(_, _, _ string, done func(any, error)) {
	done(nil, platform.ErrUnsupported)
}
