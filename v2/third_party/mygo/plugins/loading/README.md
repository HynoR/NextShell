# loading

The 29 loading indicators of [loading.dev](https://loading.dev) for
[MyGo](https://github.com/egoist/mygo) apps of native UI: arcs, rings,
dots, bars and blocks that MyGo draws itself, as loading.dev's look and
move.

```go
import "github.com/egoist/mygo/plugins/loading"

loading.Arc(c).Label("Saving")
loading.Ring(c, loading.Options{Size: 32, Color: t.Accent, Easing: loading.EaseInOut})
```

See [the documentation](../../docs/plugins/loading.md), and
`examples/loading`.

The indicators follow loading.dev's SVG and CSS (`loading-dev` 0.3.5,
MIT, © Jakub Krehel; see [NOTICE.md](NOTICE.md)): their shapes in the
same viewBoxes and fractions of their size, their keyframes, staggers and
timing functions, and their poses for reduced motion. Compared with
loading-dev rendered by a browser at the same points of their cycles,
they differ only where the browser snaps borders to pixels (Atom's
rings), in antialiasing, and on macOS in the corners of rounded squares
(Blocks, Gather, Morph, Swirl, Flip), which the renderers draw
continuous, as macOS does, so that they need no mask rasterized every
frame.
