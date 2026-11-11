package contentrules

import (
	"bytes"
	"encoding/json"
	"strings"
)

// cosmeticEntry is a css-display-none rule, or several of the same
// trigger in a row, or an ignore-previous-rules rule, as the script
// matches them against the document's URL.
type cosmeticEntry struct {
	Selectors    []string `json:"s,omitempty"`
	URL          string   `json:"u,omitempty"`
	Sensitive    bool     `json:"c,omitempty"`
	IfDomain     []string `json:"d,omitempty"`
	UnlessDomain []string `json:"n,omitempty"`
	IfTopURL     []string `json:"t,omitempty"`
	UnlessTopURL []string `json:"nt,omitempty"`
	Ignore       bool     `json:"i,omitempty"`

	trigger json.RawMessage
}

// appendCosmetic adds a rule to the entries, to the last one when that
// hides elements for the same trigger.
func appendCosmetic(entries []cosmeticEntry, trigger json.RawMessage, e cosmeticEntry, selector string) []cosmeticEntry {
	if e.URL == ".*" {
		e.URL = ""
	}
	if !e.Ignore {
		if n := len(entries); n > 0 && !entries[n-1].Ignore && bytes.Equal(entries[n-1].trigger, trigger) {
			entries[n-1].Selectors = append(entries[n-1].Selectors, selector)
			return entries
		}
		e.Selectors = []string{selector}
	}
	e.trigger = trigger
	return append(entries, e)
}

func hasHide(entries []cosmeticEntry) bool {
	for _, e := range entries {
		if !e.Ignore {
			return true
		}
	}
	return false
}

// HidingScript returns JavaScript that hides, in a document of a page applying
// the lists, the elements their css-display-none rules select for its
// URL, or "" when the lists hide nothing. Each selector gets a style rule
// of its own, so that one the engine does not take leaves the others be.
func HidingScript(lists []*List) string {
	var data []string
	for _, l := range lists {
		if l.cosmetic != "" {
			data = append(data, l.cosmetic)
		}
	}
	if len(data) == 0 {
		return ""
	}
	return "(()=>{const lists=[" + strings.Join(data, ",") + "];" + cosmeticScript + "})();"
}

// cosmeticScript hides what lists select, through a style sheet the
// document adopts, which needs no element to exist yet, and which a page
// setting the document's adoptedStyleSheets keeps. Unlike WebKit's
// user style sheet, it is the page's own: an element's inline style with
// !important wins over it. The domains of
// the rules are the page's, as in WebKit, which a frame of another
// origin knows by its referrer.
const cosmeticScript = `const url=location.href;
let top;try{top=window.top.location.href}catch(e){top=document.referrer||url}
let host;try{host=new URL(top).hostname.toLowerCase().replace(/\.$/,"")}catch(e){host=location.hostname}
const listed=ds=>ds.some(d=>d[0]==="*"?host===d.slice(1)||host.endsWith("."+d.slice(1)):host===d);
const test=(src,s,c)=>new RegExp(src,c?"":"i").test(s);
const css=[];
for(const list of lists){let hidden=[];for(const e of list){
if(e.u&&!test(e.u,url,e.c))continue;
if(e.d&&!listed(e.d)||e.n&&listed(e.n))continue;
if(e.t&&!e.t.some(t=>test(t,top,e.c))||e.nt&&e.nt.some(t=>test(t,top,e.c)))continue;
if(e.i)hidden=[];else hidden.push(...e.s)}
for(const s of hidden)css.push(s+"{display:none!important}")}
if(css.length===0)return;
const sheet=new CSSStyleSheet();sheet.replaceSync(css.join("\n"));
const adopted=Object.getOwnPropertyDescriptor(Document.prototype,"adoptedStyleSheets");
adopted.set.call(document,[...adopted.get.call(document),sheet]);
Object.defineProperty(document,"adoptedStyleSheets",{configurable:true,enumerable:true,
get(){return adopted.get.call(this)},
set(v){adopted.set.call(this,Array.prototype.includes.call(v,sheet)?v:[...v,sheet])}});`
