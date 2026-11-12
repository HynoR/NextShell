//go:build interop

package interop

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	code := m.Run()
	Cleanup()
	os.Exit(code)
}
