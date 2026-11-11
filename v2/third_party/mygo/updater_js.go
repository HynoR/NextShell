//go:build js

package mygo

import (
	"context"
	"errors"
	"io"
	"time"
)

// Browsers load the app's newest version with the page: they neither
// update nor relaunch it, and builds for them leave out the updater, with
// net/http and the signatures' cryptography.

// UpdaterModule installs new versions of the app. In browsers updates are
// disabled.
type UpdaterModule struct{}

// Updater installs new versions of the app.
var Updater = &UpdaterModule{}

// ErrUpdatesDisabled is returned by Check for builds without updates, as
// those for browsers.
var ErrUpdatesDisabled = errors.New("mygo: updates are not enabled for this build")

// Update is a newer version of the app, found by Updater.Check.
type Update struct {
	// Version of the update, and its release notes in Markdown.
	Version string
	Notes   string
	// Date is when the update was built.
	Date time.Time
}

// Enabled reports whether the app can update itself: not in browsers.
func (u *UpdaterModule) Enabled() bool { return false }

// Check returns ErrUpdatesDisabled.
func (u *UpdaterModule) Check(ctx context.Context) (*Update, error) {
	return nil, ErrUpdatesDisabled
}

// UpdateSource is where Updater gets updates from instead of the feed that
// mygo.config.ts configures. Browsers do not update apps.
type UpdateSource interface {
	// Manifest returns the manifest of the running platform.
	Manifest(ctx context.Context, prereleases bool) (io.ReadCloser, error)
	// Open returns the file at url, which the manifest names.
	Open(ctx context.Context, url string) (io.ReadCloser, error)
}

// SetSource does nothing: browsers do not update apps.
func (u *UpdaterModule) SetSource(s UpdateSource) {}

// SetPrereleases does nothing: browsers do not update apps.
func (u *UpdaterModule) SetPrereleases(on bool) {}

// Prereleases reports false.
func (u *UpdaterModule) Prereleases() bool { return false }

// Install returns ErrUpdatesDisabled.
func (up *Update) Install(ctx context.Context, progress func(downloaded, total int64)) error {
	return ErrUpdatesDisabled
}

func relaunchNow() {}

func cleanUpdateLeftovers() {}
