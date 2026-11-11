//go:build !mygo_noinspector

package automation

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// maxLine is the longest request a line may hold, as a script to
// evaluate.
const maxLine = 16 << 20

// request is a line a client sends.
type request struct {
	// ID is any JSON value, which the response repeats.
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// response is the line that answers a request: its Result, or its Error.
type response struct {
	ID     json.RawMessage `json:"id"`
	Result any             `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Error is a request's failure, with a code clients test and a message
// people read.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// The codes of Error.
const (
	// codeParse: the line is not a JSON object.
	codeParse = "parse_error"
	// codeInvalid: the request lacks a method, or its params are not the
	// method's.
	codeInvalid = "invalid_request"
	// codeUnknownMethod: no such method.
	codeUnknownMethod = "unknown_method"
	// codeNoWindow: no window has the ID, or the app has none.
	codeNoWindow = "no_window"
	// codeNoElement: no element of the window's last frame has the ID or
	// the key.
	codeNoElement = "no_element"
	// codeUnsupported: the window or the platform cannot do it, as
	// reading the elements of a page.
	codeUnsupported = "unsupported"
	// codeTimeout: the window did not settle in the time idle allowed.
	codeTimeout = "timeout"
	// codeFailed: the app could not do it, as a script that threw.
	codeFailed = "failed"
)

func errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// driver is what requests do in the app. Window 0 is the default window:
// the focused one, else the first. Methods are called from the goroutines
// of connections, and hop to the main thread for what they touch.
type driver interface {
	windows() ([]windowInfo, error)
	tree(window int) (int, *platform.AccessTree, map[uint64]any, error)
	act(window int, ev platform.SurfaceEvent) error
	input(window int, evs []platform.SurfaceEvent) error
	menu(window int, path []string) error
	capture(window int) ([]byte, error)
	eval(window int, script string) (any, error)
	idle(window int, timeout time.Duration) error
}

// serve answers the connections of l until it closes.
func serve(l net.Listener, d driver) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go serveConn(c, d)
	}
}

// serveConn answers the requests of a connection in order, each once the
// one before it is answered, until the client closes it.
func serveConn(c io.ReadWriteCloser, d driver) {
	defer c.Close()
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	enc := json.NewEncoder(c)
	enc.SetEscapeHTML(false)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if enc.Encode(handle(d, line)) != nil {
			return
		}
	}
	if errors.Is(sc.Err(), bufio.ErrTooLong) {
		enc.Encode(response{Error: errorf(codeParse, "a request is longer than %d bytes", maxLine)})
	}
}

// handle answers a line.
func handle(d driver, line []byte) response {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return response{Error: errorf(codeParse, "%v", err)}
	}
	res := response{ID: req.ID}
	v, err := call(d, req)
	if err != nil {
		var e *Error
		if !errors.As(err, &e) {
			e = &Error{Code: codeFailed, Message: err.Error()}
		}
		res.Error = e
		return res
	}
	res.Result = v
	return res
}

// done is the result of requests that answer nothing but success.
type done struct{}

// call performs a request.
func call(d driver, req request) (any, error) {
	switch req.Method {
	case "":
		return nil, errorf(codeInvalid, "the request has no method")
	case "windows":
		if err := params(req, &struct{}{}); err != nil {
			return nil, err
		}
		ws, err := d.windows()
		if err != nil {
			return nil, err
		}
		return windowsResult{Windows: ws}, nil
	case "tree":
		var p windowParams
		if err := params(req, &p); err != nil {
			return nil, err
		}
		w, t, keys, err := d.tree(p.Window)
		if err != nil {
			return nil, err
		}
		return treeJSON(w, t, keys), nil
	case "act":
		var p actParams
		if err := params(req, &p); err != nil {
			return nil, err
		}
		return act(d, p)
	case "input":
		var p inputParams
		if err := params(req, &p); err != nil {
			return nil, err
		}
		evs, err := surfaceEvents(p.Events)
		if err != nil {
			return nil, err
		}
		return done{}, d.input(p.Window, evs)
	case "menu":
		var p menuParams
		if err := params(req, &p); err != nil {
			return nil, err
		}
		if len(p.Path) == 0 {
			return nil, errorf(codeInvalid, "menu needs the path of the item, as [\"File\", \"New\"]")
		}
		return done{}, d.menu(p.Window, p.Path)
	case "capture":
		var p windowParams
		if err := params(req, &p); err != nil {
			return nil, err
		}
		b, err := d.capture(p.Window)
		if err != nil {
			return nil, err
		}
		r := captureResult{PNG: b}
		if c, err := png.DecodeConfig(bytes.NewReader(b)); err == nil {
			r.Width, r.Height = c.Width, c.Height
		}
		return r, nil
	case "eval":
		var p evalParams
		if err := params(req, &p); err != nil {
			return nil, err
		}
		v, err := d.eval(p.Window, p.Script)
		if err != nil {
			return nil, err
		}
		return evalResult{Value: v}, nil
	case "idle":
		var p idleParams
		if err := params(req, &p); err != nil {
			return nil, err
		}
		if p.Timeout < 0 {
			return nil, errorf(codeInvalid, "timeout is negative")
		}
		return done{}, d.idle(p.Window, time.Duration(p.Timeout)*time.Millisecond)
	}
	return nil, errorf(codeUnknownMethod, "no method %q", req.Method)
}

// params decodes the params of a request into p, refusing fields the
// method does not take.
func params(req request, p any) error {
	raw := bytes.TrimSpace(req.Params)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(p); err != nil {
		return errorf(codeInvalid, "params of %s: %v", req.Method, err)
	}
	return nil
}

type windowParams struct {
	Window int `json:"window"`
}

type actParams struct {
	Window int    `json:"window"`
	ID     nodeID `json:"id"`
	// Key finds the element by its key instead of ID.
	Key    json.RawMessage `json:"key"`
	Action string          `json:"action"`
	// Text is setValue's and insertText's; From and To are selectText's.
	Text string `json:"text"`
	From int    `json:"from"`
	To   int    `json:"to"`
}

type inputParams struct {
	Window int          `json:"window"`
	Events []inputEvent `json:"events"`
}

type menuParams struct {
	Window int      `json:"window"`
	Path   []string `json:"path"`
}

type evalParams struct {
	Window int    `json:"window"`
	Script string `json:"script"`
}

type idleParams struct {
	Window int `json:"window"`
	// Timeout is in milliseconds, 0 for none.
	Timeout int `json:"timeout"`
}

type windowsResult struct {
	Windows []windowInfo `json:"windows"`
}

// windowInfo describes a window.
type windowInfo struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	// Kind is "content" for a window of native UI, "page" for a web page.
	Kind string `json:"kind"`
	// URL is a page's.
	URL           string `json:"url,omitempty"`
	Bounds        rect   `json:"bounds"`
	ContentBounds rect   `json:"contentBounds"`
	Focused       bool   `json:"focused"`
	Visible       bool   `json:"visible"`
}

type rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type captureResult struct {
	// PNG is encoded in base64, as encoding/json does []byte; Width and
	// Height are its size in pixels.
	PNG    []byte `json:"png"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type evalResult struct {
	Value any `json:"value"`
}

// nodeID is the ID of an element. It goes as a string of its decimal
// digits, which JavaScript's numbers would round, and comes as one or as
// a number.
type nodeID uint64

func (id nodeID) MarshalJSON() ([]byte, error) {
	return strconv.AppendQuote(nil, strconv.FormatUint(uint64(id), 10)), nil
}

func (id *nodeID) UnmarshalJSON(b []byte) error {
	s := string(b)
	if u, err := strconv.Unquote(s); err == nil {
		s = u
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return fmt.Errorf("an element ID is a string of digits, not %s", b)
	}
	*id = nodeID(n)
	return nil
}

// act performs an action of assistive technology on the element of the
// ID or the key.
func act(d driver, p actParams) (any, error) {
	kind, ok := actionKinds[p.Action]
	if !ok {
		return nil, errorf(codeInvalid, "no action %q", p.Action)
	}
	hasKey := len(bytes.TrimSpace(p.Key)) > 0
	switch {
	case p.ID == 0 && !hasKey:
		return nil, errorf(codeInvalid, "act needs the id or the key of an element")
	case p.ID != 0 && hasKey:
		return nil, errorf(codeInvalid, "act takes an id or a key, not both")
	}
	id := uint64(p.ID)
	if hasKey {
		var want bytes.Buffer
		if err := json.Compact(&want, p.Key); err != nil {
			return nil, errorf(codeInvalid, "key: %v", err)
		}
		_, t, keys, err := d.tree(p.Window)
		if err != nil {
			return nil, err
		}
		n := 0
		for _, node := range t.Nodes {
			k, ok := keys[node.ID]
			if !ok {
				continue
			}
			if b, err := json.Marshal(keyJSON(k)); err == nil && bytes.Equal(b, want.Bytes()) {
				id = node.ID
				n++
			}
		}
		switch {
		case n == 0:
			return nil, errorf(codeNoElement, "no element has the key %s", want.Bytes())
		case n > 1:
			return nil, errorf(codeInvalid, "%d elements have the key %s: act on one by its id", n, want.Bytes())
		}
	}
	ev := platform.SurfaceEvent{Kind: platform.AccessAction, ID: id, Action: kind, Text: p.Text, From: p.From, To: p.To}
	return done{}, d.act(p.Window, ev)
}

// treeJSON is the tree request's answer: the elements of window's last
// frame.
func treeJSON(window int, t *platform.AccessTree, keys map[uint64]any) treeResult {
	r := treeResult{Window: window, Focus: nodeID(t.Focus), Nodes: make([]nodeJSON, 0, len(t.Nodes)), Announcements: t.Announcements}
	for _, n := range t.Nodes {
		j := nodeJSON{
			ID: nodeID(n.ID), Role: roleName(n.Role), Label: n.Label, Value: n.Value,
			Placeholder: n.Placeholder, Description: n.Description, KeyShortcuts: n.KeyShortcuts,
			Bounds:  rect{n.Bounds.X, n.Bounds.Y, n.Bounds.W, n.Bounds.H},
			States:  names(uint64(n.States), stateNames[:]),
			Actions: names(uint64(n.Actions), actionNames[:]),
			Level:   n.Level, PosInSet: n.PosInSet, SetSize: n.SetSize,
			RowCount: n.RowCount, ColCount: n.ColCount, RowIndex: n.RowIndex, ColIndex: n.ColIndex,
			RowSpan: n.RowSpan, ColSpan: n.ColSpan,
			Current: n.Current.String(), AutoComplete: n.AutoComplete.String(), WebView: n.WebView != nil,
			RoleDescription: n.RoleDescription, HasPopup: n.HasPopup.String(),
		}
		for _, c := range n.Controls {
			j.Controls = append(j.Controls, nodeID(c))
		}
		for _, d := range n.DescribedBy {
			j.DescribedBy = append(j.DescribedBy, nodeID(d))
		}
		if n.States&platform.AccessInvalid != 0 {
			for _, m := range n.ErrorMessage {
				j.ErrorMessage = append(j.ErrorMessage, nodeID(m))
			}
		}
		if n.Parent >= 0 && n.Parent < len(t.Nodes) {
			j.Parent = nodeID(t.Nodes[n.Parent].ID)
		}
		if k, ok := keys[n.ID]; ok {
			j.Key = keyJSON(k)
		}
		if n.Role.Ranged() {
			j.Min, j.Max, j.Now, j.Step = &n.Min, &n.Max, &n.Now, &n.Step
		}
		if n.Role == platform.RoleTextField || n.Role == platform.RoleComboBox {
			j.Selection = &[2]int{n.SelStart, n.SelEnd}
		}
		r.Nodes = append(r.Nodes, j)
	}
	return r
}

type treeResult struct {
	Window int `json:"window"`
	// Focus is the element with the keyboard focus.
	Focus         nodeID     `json:"focus,omitempty"`
	Nodes         []nodeJSON `json:"nodes"`
	Announcements []string   `json:"announcements,omitempty"`
}

// nodeJSON is an element, as assistive technology reads it.
type nodeJSON struct {
	ID nodeID `json:"id"`
	// Parent is omitted for the elements at the top.
	Parent nodeID `json:"parent,omitempty"`
	Role   string `json:"role"`
	// Key is the key the app gave the element (ui's Key).
	Key         any      `json:"key,omitempty"`
	Label       string   `json:"label,omitempty"`
	Value       string   `json:"value,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Description string   `json:"description,omitempty"`
	Bounds      rect     `json:"bounds"`
	States      []string `json:"states,omitempty"`
	Actions     []string `json:"actions,omitempty"`
	// Min, Max, Now and Step are those of a slider, a progress bar, a
	// meter or a stepper.
	Min  *float64 `json:"min,omitempty"`
	Max  *float64 `json:"max,omitempty"`
	Now  *float64 `json:"now,omitempty"`
	Step *float64 `json:"step,omitempty"`
	// KeyShortcuts are the keys that press or focus the element, as
	// ARIA's aria-keyshortcuts, as the platform's menus write them
	// ("⌘K" on macOS, "Ctrl+K" elsewhere), separated by spaces.
	KeyShortcuts string `json:"keyShortcuts,omitempty"`
	// Selection is a text field's, in runes.
	Selection *[2]int `json:"selection,omitempty"`
	Level     int     `json:"level,omitempty"`
	PosInSet  int     `json:"posInSet,omitempty"`
	SetSize   int     `json:"setSize,omitempty"`
	// RowCount and ColCount are how many rows and columns a table or a
	// grid has, and RowIndex and ColIndex the place of a row or a cell
	// among them, from 1, as the app gave them (aria-rowcount,
	// aria-colcount, aria-rowindex, aria-colindex).
	RowCount int `json:"rowCount,omitempty"`
	ColCount int `json:"colCount,omitempty"`
	RowIndex int `json:"rowIndex,omitempty"`
	ColIndex int `json:"colIndex,omitempty"`
	// RowSpan and ColSpan are how many rows and columns a cell covers,
	// when more than one (aria-rowspan, aria-colspan).
	RowSpan int `json:"rowSpan,omitempty"`
	ColSpan int `json:"colSpan,omitempty"`
	// Current is the item the element is the current one of, as ARIA's
	// aria-current: true, page, step, location, date or time.
	Current string `json:"current,omitempty"`
	// AutoComplete is what the suggestions of a text field or a combo
	// box complete as the user types, as ARIA's aria-autocomplete:
	// inline, list or both.
	AutoComplete string `json:"autoComplete,omitempty"`
	// RoleDescription is what the app calls the element's role, as
	// ARIA's aria-roledescription; HasPopup the kind of popup it opens,
	// as aria-haspopup: menu, listbox, tree, grid or dialog; Controls the
	// elements it controls, as aria-controls.
	RoleDescription string   `json:"roleDescription,omitempty"`
	HasPopup        string   `json:"hasPopup,omitempty"`
	Controls        []nodeID `json:"controls,omitempty"`
	// DescribedBy are the elements whose text describes the element, as
	// ARIA's aria-describedby, and ErrorMessage those holding its error
	// while it is invalid, as aria-errormessage.
	DescribedBy  []nodeID `json:"describedBy,omitempty"`
	ErrorMessage []nodeID `json:"errorMessage,omitempty"`
	// WebView marks the node of a web view's page, whose elements the
	// tree leaves out.
	WebView bool `json:"webView,omitempty"`
}

// keyJSON is how a key goes in JSON: strings, booleans and integers as
// themselves, other keys as fmt prints them.
func keyJSON(k any) any {
	switch k := k.(type) {
	case string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr:
		return k
	}
	return fmt.Sprint(k)
}
