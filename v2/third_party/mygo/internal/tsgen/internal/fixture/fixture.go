// Package fixture declares types used to test the TypeScript generator.
package fixture

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Status is the state of a task.
type Status string

// Task states.
const (
	StatusTodo Status = "todo"
	StatusDone Status = "done"
)

// Priority orders tasks.
type Priority int

// Priorities.
const (
	Low Priority = iota
	Medium
	High
)

// Timestamps is embedded in records.
type Timestamps struct {
	CreatedAt time.Time `json:"createdAt"`
}

// Channel groups tasks, and shares its name with the runtime's stream type.
type Channel struct {
	Name string `json:"name"`
}

// Record has a map that uses TypeScript's Record utility type.
type Record struct {
	Values map[string]string `json:"values"`
}

// Promise shares its name with the return type of generated methods.
type Promise struct {
	Done bool `json:"done"`
}

// Task is a unit of work.
type Task struct {
	ID int64 `json:"id"`
	// Title of the task.
	Title    string           `json:"title"`
	Status   Status           `json:"status"`
	Priority Priority         `json:"priority"`
	Tags     []string         `json:"tags"`
	Due      *time.Time       `json:"due"`
	Meta     map[string]any   `json:"meta,omitempty"`
	Parent   *Task            `json:"parent,omitzero"`
	Channel  *Channel         `json:"channel,omitzero"`
	Record   *Record          `json:"record,omitzero"`
	Promise  *Promise         `json:"promise,omitzero"`
	Secret   string           `json:"-"`
	Count    int64            `json:"count,string"`
	CountPtr *int64           `json:"countPtr,string"`
	Raw      json.RawMessage  `json:"raw"`
	Data     []byte           `json:"data"`
	Scores   map[int]float64  `json:"scores"`
	Matrix   [][]int          `json:"matrix"`
	Children []*Task          `json:"children"`
	Extra    struct{ A bool } `json:"extra"`
	NoTag    string
	internal int
	Timestamps
}

// Page is a page of results.
type Page[T any] struct {
	Items []T    `json:"items"`
	Next  string `json:"next,omitempty"`
}

// Tasks manages tasks.
type Tasks struct{}

// List returns tasks with the given status.
func (Tasks) List(ctx context.Context, status Status, limit int) (Page[Task], error) {
	return Page[Task]{}, nil
}

// Add adds tasks.
//
// It fails when a task has no title.
func (*Tasks) Add(tasks ...Task) error {
	for _, t := range tasks {
		if t.Title == "" {
			return errors.New("missing title")
		}
	}
	return nil
}

// Count counts tasks.
func (Tasks) Count() int { return 0 }

// Delete removes a task; the parameter is unnamed on purpose.
func (Tasks) Delete(int64) {}

// Dispatch deliberately names parameters after the runtime's call function.
func (Tasks) Dispatch(call, call_ string) {}

// DispatchMany uses the runtime's call name for a variadic parameter.
func (Tasks) DispatchMany(call ...string) {}

// Watch sends the tasks with the given status as they change. The test
// declares updates a channel of tasks.
func (Tasks) Watch(status Status, updates Task) {}
