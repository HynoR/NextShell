package desktop

import (
	"context"
	"github.com/egoist/mygo"
	"nextshell.dev/v2/internal/remoteedit"
)

type editorWatch struct{ w *mygo.FileWatcher }

func (w editorWatch) Next(ctx context.Context) error { _, e := w.w.Next(ctx); return e }
func (w editorWatch) Close() error                   { return w.w.Close() }
func watchEditor(ctx context.Context, path string) (remoteedit.Watcher, error) {
	w, e := mygo.WatchFiles(ctx, path, mygo.FileWatchOptions{})
	if e != nil {
		return nil, e
	}
	return editorWatch{w}, nil
}
