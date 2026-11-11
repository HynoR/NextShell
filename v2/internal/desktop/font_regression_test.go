package desktop

import (
	"fmt"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"math"
	"runtime"
	"testing"
)

func TestCJKFontCellMetrics(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux font policy")
	}
	p, e := Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer p.Shutdown()
	after := p.terminalOptions(terminal.Options{}).Font
	for _, f := range []terminal.Font{{Family: "monospace", Size: 13}, after} {
		style := ui.Font{Family: f.Family, Size: f.Size}
		adv := float32(0)
		for _, g := range ui.Shape(" !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~", style) {
			adv = max(adv, g.Advance)
		}
		wide := float32(0)
		for _, g := range ui.Shape("中", style) {
			wide += g.Advance
		}
		cell := math.Ceil(float64(adv - .05))
		gap := 2*cell - float64(wide)
		fmt.Printf("FONT_PROBE family=%q size=%.0f ascii=%.3f cell=%.0f CJK=%.3f gap=%.3f\n", f.Family, f.Size, adv, cell, wide, gap)
		if f.Family == "Noto Sans Mono CJK SC" && math.Abs(gap) > .1 {
			t.Fatal("CJK font is not aligned to two cells")
		}
	}
}
