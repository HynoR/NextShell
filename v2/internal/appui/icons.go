// SPDX-License-Identifier: MIT
// Copyright (c) 2026 NextShell contributors
// Original minimal SVG geometry; no external icon package or font is required.
package appui

import "github.com/egoist/mygo/ui"

func iconSVG(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

var appIcons = map[string]*ui.SVG{
	"settings":      iconSVG(`<circle cx="12" cy="12" r="3"/><path d="M12 2v3m0 14v3M2 12h3m14 0h3M4.9 4.9 7 7m10 10 2.1 2.1M4.9 19.1 7 17m10-10 2.1-2.1"/><circle cx="12" cy="12" r="7"/>`),
	"diagnostics":   iconSVG(`<circle cx="12" cy="12" r="8"/><path d="M12 1v5m0 12v5M1 12h5m12 0h5m-9-11-4 11h5l-4 9"/>`),
	"terminal":      iconSVG(`<rect x="3" y="4" width="18" height="16" rx="3"/><path d="m7 9 3 3-3 3m6 0h4"/>`),
	"server":        iconSVG(`<rect x="4" y="3" width="16" height="7" rx="2"/><rect x="4" y="14" width="16" height="7" rx="2"/><path d="M8 6.5h.1M8 17.5h.1"/>`),
	"folder":        iconSVG(`<path d="M3 8V6a2 2 0 0 1 2-2h5l3 3h6a2 2 0 0 1 2 2v10H3z"/>`),
	"file":          iconSVG(`<path d="M6 3h8l5 5v13H6zM14 3v6h5"/>`),
	"keys":          iconSVG(`<circle cx="8" cy="9" r="5"/><path d="m12 13 8 8m-4-4 3-3m-6 0 3-3"/>`),
	"proxies":       iconSVG(`<circle cx="5" cy="12" r="3"/><circle cx="19" cy="5" r="3"/><circle cx="19" cy="19" r="3"/><path d="m8 10 8-4m-8 8 8 4"/>`),
	"data":          iconSVG(`<path d="M12 3v12m-4-4 4 4 4-4M4 16v5h16v-5"/>`),
	"cloud":         iconSVG(`<path d="M7 18a5 5 0 0 1-1-10 6 6 0 0 1 11-2 6 6 0 0 1 1 12z"/>`),
	"agent":         iconSVG(`<rect x="4" y="6" width="16" height="14" rx="4"/><path d="M12 2v4m-5 6h1m8 0h1m-9 4h6M1 11v4m22-4v4"/>`),
	"trash":         iconSVG(`<path d="M4 7h16M9 7V4h6v3m-9 0 1 14h10l1-14M10 11v6m4-6v6"/>`),
	"info":          iconSVG(`<circle cx="12" cy="12" r="9"/><path d="M12 11v6m0-9.5h.01"/>`),
	"search":        iconSVG(`<circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/>`),
	"plus":          iconSVG(`<path d="M12 5v14M5 12h14"/>`),
	"close":         iconSVG(`<path d="M6 6l12 12M18 6 6 18"/>`),
	"up":            iconSVG(`<path d="M12 19V5m-6 6 6-6 6 6"/>`),
	"refresh":       iconSVG(`<path d="M20 11a8 8 0 1 0-2 6m2-12v6h-6"/>`),
	"upload":        iconSVG(`<path d="M12 16V4m-5 5 5-5 5 5M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/>`),
	"edit":          iconSVG(`<path d="M4 20h4L19 9l-4-4L4 16v4Zm9-13 4 4"/>`),
	"panel-left":    iconSVG(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/>`),
	"panel-right":   iconSVG(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M15 4v16"/>`),
	"chevron-down":  iconSVG(`<path d="m6 9 6 6 6-6"/>`),
	"chevron-right": iconSVG(`<path d="m9 6 6 6-6 6"/>`),
	"chevron-up":    iconSVG(`<path d="m6 15 6-6 6 6"/>`),
	"transfer":      iconSVG(`<path d="M7 20V4M3 8l4-4 4 4m6-4v16m4-4-4 4-4-4"/>`),
	"cpu":           iconSVG(`<rect x="6" y="6" width="12" height="12" rx="2"/><path d="M9 2v4m6-4v4M9 18v4m6-4v4M2 9h4m-4 6h4m12-6h4m-4 6h4"/>`),
	"globe":         iconSVG(`<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/>`),
	"play":          iconSVG(`<path d="M7 4.5v15l12-7.5z"/>`),
	"more":          iconSVG(`<path d="M5 12h.01M12 12h.01M19 12h.01" stroke-width="3"/>`),
}
