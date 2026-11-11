package automationcli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// client sends requests to an app's socket, one at a time, as
// docs/plugins/automation.md describes.
type client struct {
	conn net.Conn
	r    *bufio.Reader
	n    int
}

// dial connects to the socket at path. A request that the app does not
// answer by deadline fails, unless deadline is zero.
func dial(path string, deadline time.Time) (*client, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the app at %s (did it quit? is MYGO_AUTOMATION set and the plugin imported?): %v", path, err)
	}
	conn.SetDeadline(deadline)
	return &client{conn: conn, r: bufio.NewReader(conn)}, nil
}

func (c *client) Close() error { return c.conn.Close() }

// protoError is the error of an answer: a code from the protocol's table
// and its message.
type protoError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *protoError) Error() string { return e.Code + ": " + e.Message }

// call sends a request and decodes its result into result, unless nil.
// It returns the answer's error as a *protoError.
func (c *client) call(method string, params any, result any) error {
	raw, err := c.callRaw(method, params)
	if err != nil {
		return err
	}
	if result != nil {
		if err := json.Unmarshal(raw, result); err != nil {
			return fmt.Errorf("%s: the app's answer: %v", method, err)
		}
	}
	return nil
}

// callRaw sends a request and returns its result as the app wrote it.
func (c *client) callRaw(method string, params any) (json.RawMessage, error) {
	c.n++
	line, err := json.Marshal(map[string]any{"id": c.n, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if _, err := c.conn.Write(append(line, '\n')); err != nil {
		return nil, fmt.Errorf("%s: %v", method, err)
	}
	answer, err := c.r.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("%s: the app did not answer: %v", method, err)
	}
	var res struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *protoError     `json:"error"`
	}
	if err := json.Unmarshal(answer, &res); err != nil {
		return nil, fmt.Errorf("%s: the app's answer: %v", method, err)
	}
	if res.Error != nil {
		return nil, res.Error
	}
	if res.ID != c.n {
		return nil, fmt.Errorf("%s: an answer to request %d, not %d", method, res.ID, c.n)
	}
	return res.Result, nil
}

// windowInfo is a window the windows method lists.
type windowInfo struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Kind    string `json:"kind"`
	URL     string `json:"url,omitempty"`
	Focused bool   `json:"focused"`
	Visible bool   `json:"visible"`
}

type rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// tree is the tree method's result.
type tree struct {
	Window        int      `json:"window"`
	Focus         string   `json:"focus,omitempty"`
	Nodes         []node   `json:"nodes"`
	Announcements []string `json:"announcements,omitempty"`
}

// node is an element of a tree, with the fields the protocol gives.
type node struct {
	ID          string          `json:"id"`
	Parent      string          `json:"parent,omitempty"`
	Role        string          `json:"role"`
	Key         json.RawMessage `json:"key,omitempty"`
	Label       string          `json:"label,omitempty"`
	Value       string          `json:"value,omitempty"`
	Placeholder string          `json:"placeholder,omitempty"`
	Description string          `json:"description,omitempty"`
	Shortcuts   string          `json:"keyShortcuts,omitempty"`
	Bounds      rect            `json:"bounds"`
	States      []string        `json:"states,omitempty"`
	Actions     []string        `json:"actions,omitempty"`
	Min         *float64        `json:"min,omitempty"`
	Max         *float64        `json:"max,omitempty"`
	Now         *float64        `json:"now,omitempty"`
	Step        *float64        `json:"step,omitempty"`
	Selection   *[2]int         `json:"selection,omitempty"`
	Level       int             `json:"level,omitempty"`
	PosInSet    int             `json:"posInSet,omitempty"`
	SetSize     int             `json:"setSize,omitempty"`
	RowCount    int             `json:"rowCount,omitempty"`
	ColCount    int             `json:"colCount,omitempty"`
	RowIndex    int             `json:"rowIndex,omitempty"`
	ColIndex    int             `json:"colIndex,omitempty"`
	RowSpan     int             `json:"rowSpan,omitempty"`
	ColSpan     int             `json:"colSpan,omitempty"`
	Current     string          `json:"current,omitempty"`
	Complete    string          `json:"autoComplete,omitempty"`
	WebView     bool            `json:"webView,omitempty"`
	// RoleDescription and HasPopup are aria-roledescription and
	// aria-haspopup.
	RoleDescription string `json:"roleDescription,omitempty"`
	HasPopup        string `json:"hasPopup,omitempty"`
}

// inputEvent is an event of the input method.
type inputEvent struct {
	Type    string   `json:"type"`
	X       float64  `json:"x,omitempty"`
	Y       float64  `json:"y,omitempty"`
	Button  string   `json:"button,omitempty"`
	Clicks  int      `json:"clicks,omitempty"`
	DX      float64  `json:"dx,omitempty"`
	DY      float64  `json:"dy,omitempty"`
	Precise bool     `json:"precise,omitempty"`
	Key     string   `json:"key,omitempty"`
	Mods    []string `json:"mods,omitempty"`
	Text    string   `json:"text,omitempty"`
}
