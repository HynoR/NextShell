// SPDX-License-Identifier: MIT
// Copyright (c) 2026 NextShell contributors
// Original minimal SVG geometry; no external icon package or font is required.
package appui

import "github.com/egoist/mygo/ui"

func iconSVG(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

var appIcons = map[string]*ui.SVG{
	"terminal":    iconSVG(`<rect x="3" y="4" width="18" height="16" rx="3"/><path d="m7 9 3 3-3 3m6 0h4"/>`),
	"connections": iconSVG(`<rect x="4" y="3" width="16" height="7" rx="2"/><rect x="4" y="14" width="16" height="7" rx="2"/><path d="M8 6.5h.1M8 17.5h.1m4-11h5m-5 11h5"/>`),
	"folder":      iconSVG(`<path d="M3 8V6a2 2 0 0 1 2-2h5l3 3h6a2 2 0 0 1 2 2v10H3z"/>`),
	"keys":        iconSVG(`<circle cx="8" cy="9" r="5"/><path d="m12 13 8 8m-4-4 3-3m-6 0 3-3"/>`),
	"proxies":     iconSVG(`<circle cx="5" cy="12" r="3"/><circle cx="19" cy="5" r="3"/><circle cx="19" cy="19" r="3"/><path d="m8 10 8-4m-8 8 8 4"/>`),
	"files":       iconSVG(`<path d="M6 3h8l5 5v13H6zM14 3v6h5m-10 4h7m-7 4h7"/>`),
	"transfers":   iconSVG(`<path d="M7 3v16m-4-4 4 4 4-4m6 6V5m-4 4 4-4 4 4"/>`),
	"editor":      iconSVG(`<path d="m4 17 11-11 4 4L8 21H4zm10-10 4 4M4 4h7m-7 5h4"/>`),
	"monitor":     iconSVG(`<path d="M3 12h4l3-8 4 16 3-8h4"/>`),
	"commands":    iconSVG(`<path d="m4 6 5 5-5 5m9 0h7M4 21h16"/>`),
	"data":        iconSVG(`<path d="M12 3v12m-4-4 4 4 4-4M4 16v5h16v-5"/>`),
	"cloud":       iconSVG(`<path d="M7 18a5 5 0 0 1-1-10 6 6 0 0 1 11-2 6 6 0 0 1 1 12M12 11v10m-3-3 3 3 3-3"/>`),
	"agent":       iconSVG(`<rect x="4" y="6" width="16" height="14" rx="4"/><path d="M12 2v4m-5 6h1m8 0h1m-9 4h6M1 11v4m22-4v4"/>`),
	"shell":       iconSVG(`<path d="m8 6-6 6 6 6m8-12 6 6-6 6M14 3l-4 18"/>`),
	"activity":    iconSVG(`<rect x="3" y="3" width="8" height="8" rx="2"/><rect x="15" y="3" width="6" height="18" rx="2"/><rect x="3" y="15" width="8" height="6" rx="2"/>`),
	"edit":        iconSVG(`<path d="m4 17 12-12 4 4L8 21H4zm10-10 4 4"/>`),
}
