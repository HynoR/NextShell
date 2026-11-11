//go:build darwin

package metal

import (
	"errors"
	"fmt"
	"time"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/scene"
)

// RenderOffscreen draws s with a renderer of its own into a texture,
// without a layer, and returns its premultiplied BGRA rows: for tests, as
// packages drawing effects have.
func RenderOffscreen(s *scene.Scene) ([]byte, error) {
	r, err := newRenderer()
	if err != nil {
		return nil, err
	}
	defer r.Release()
	return r.renderOffscreen(s)
}

// Offscreen draws frames with a renderer of its own into a texture,
// without a layer, waiting for each and timing it on the GPU: for
// benchmarks, as those of package ui drawing its frames.
type Offscreen struct {
	r    *Renderer
	tex  id
	w, h int
	// encoded is how long encoding the last frame took on the CPU.
	encoded time.Duration
}

var msgDouble func(obj id, sel uintptr) float64

// NewOffscreen returns an Offscreen drawing frames of w×h pixels.
func NewOffscreen(w, h int) (*Offscreen, error) {
	r, err := newRenderer()
	if err != nil {
		return nil, err
	}
	if msgDouble == nil {
		purego.RegisterFunc(&msgDouble, msgSend)
	}
	o := &Offscreen{r: r}
	pool(func() { err = o.target(w, h) })
	if err != nil {
		o.Release()
		return nil, err
	}
	return o, nil
}

// target makes the texture drawn into w×h pixels.
func (o *Offscreen) target(w, h int) error {
	if o.tex != 0 && o.w == w && o.h == h {
		return nil
	}
	release(&o.tex)
	o.r.cur = &o.r.formats[0]
	o.tex = o.r.newTexture(w, h, pixelFormatBGRA8Unorm, usageRenderTarget|usageShaderRead, nil, 0)
	if o.tex == 0 {
		return errors.New("metal: cannot create a target")
	}
	o.w, o.h = w, h
	return nil
}

// Render draws s, a texture of its size taking the place of one of
// another, waits for the GPU to finish it, and returns the time the GPU
// spent on it, by its command buffer's timestamps.
func (o *Offscreen) Render(s *scene.Scene) (gpu time.Duration, err error) {
	pool(func() {
		r := o.r
		r.waitLast()
		if err = o.target(s.Width, s.Height); err != nil {
			return
		}
		var cb id
		start := time.Now()
		if cb, err = r.encode(s, o.tex); err != nil {
			return
		}
		o.encoded = time.Since(start)
		send(cb, "commit")
		send(cb, "waitUntilCompleted")
		if int(send(cb, "status")) == statusError {
			err = fmt.Errorf("metal: the frame failed: %s", describe(send(cb, "error")))
			return
		}
		secs := msgDouble(cb, uintptr(sel("GPUEndTime"))) - msgDouble(cb, uintptr(sel("GPUStartTime")))
		gpu = time.Duration(secs * float64(time.Second))
	})
	return gpu, err
}

// Encoded returns how long the CPU took to encode the last frame Render
// drew: building its instances (gpu.Builder), uploading what changed in
// its atlases, and encoding its passes, before committing them.
func (o *Offscreen) Encoded() time.Duration { return o.encoded }

// Release frees the texture and the renderer.
func (o *Offscreen) Release() {
	pool(func() { release(&o.tex) })
	o.r.Release()
}
