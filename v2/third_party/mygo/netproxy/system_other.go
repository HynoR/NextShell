//go:build !windows && !(darwin && (amd64 || arm64)) && !(linux && (amd64 || arm64))

package netproxy

import "net/url"

// In browsers the browser makes the requests, through its own proxy; other
// systems go direct.
func system(*url.URL) *url.URL { return nil }
