//go:build linux && (amd64 || arm64)

package linux

import (
	"os"
	"path/filepath"
	"strings"
)

// WebKitGTK draws pages with its DMA-BUF renderer where it can, which
// shows blank windows on NVIDIA's driver, in virtual machines and in
// containers, so MyGo turns it off (WEBKIT_DISABLE_DMABUF_RENDERER=1)
// before GTK starts. Pages then draw on the CPU, which scrolls long pages
// at a fraction of the frame rate on hosts where the renderer works:
// App.SetDMABUFRenderer leaves it on, everywhere ("on") or where the host
// is known to draw with it ("auto").

// dmabufProbe is dmabufSafe, which LinkDMABUFProbe links: Init is linked
// into every app, and only calls it.
var dmabufProbe func(getenv func(string) string, root string) bool

// LinkDMABUFProbe links the code that looks at the host, when the app asks
// for "auto".
func LinkDMABUFProbe() { dmabufProbe = dmabufSafe }

// disableDMABUF reports whether Init sets WEBKIT_DISABLE_DMABUF_RENDERER=1
// for the renderer the app asked for, with getenv the environment and root
// the file system's root. The variable, set by the user, always wins.
func disableDMABUF(renderer string, getenv func(string) string, root string) bool {
	if getenv("WEBKIT_DISABLE_DMABUF_RENDERER") != "" {
		return false
	}
	switch renderer {
	case "on":
		return false
	case "auto":
		return dmabufProbe == nil || !dmabufProbe(getenv, root)
	}
	return true
}

// dmabufSafe reports a host where the DMA-BUF renderer draws: GTK will
// open a Wayland display, and OpenGL runs on a GPU of Mesa's, outside
// containers and virtual machines.
func dmabufSafe(getenv func(string) string, root string) bool {
	// GDK_BACKEND lists the backends GTK tries, Wayland first by default.
	if b, _, _ := strings.Cut(getenv("GDK_BACKEND"), ","); b != "" && b != "*" && b != "wayland" {
		return false
	}
	// A display whose socket is gone (a stale variable) does not count.
	socket := getenv("WAYLAND_DISPLAY")
	if socket == "" {
		return false
	}
	if !filepath.IsAbs(socket) {
		dir := getenv("XDG_RUNTIME_DIR")
		if dir == "" {
			return false
		}
		socket = filepath.Join(dir, socket)
	}
	if fi, err := os.Stat(socket); err != nil || fi.Mode()&os.ModeSocket == 0 {
		return false
	}
	// Software OpenGL (llvmpipe).
	if s := strings.ToLower(getenv("LIBGL_ALWAYS_SOFTWARE")); s != "" && s != "0" && s != "false" {
		return false
	}
	gallium := getenv("GALLIUM_DRIVER")
	if gallium == "llvmpipe" || gallium == "softpipe" {
		return false
	}
	// NVIDIA's driver, a container, WSL.
	for _, p := range []string{"sys/module/nvidia", "proc/driver/nvidia/version", "dev/nvidiactl", ".dockerenv", "run/.containerenv", "dev/dxg"} {
		if _, err := os.Stat(filepath.Join(root, p)); err == nil {
			return false
		}
	}
	// A virtual machine, by its firmware's product or vendor (QEMU names
	// itself only as the vendor, Hyper-V's product is "Virtual Machine").
	for _, f := range []string{"product_name", "sys_vendor"} {
		b, _ := os.ReadFile(filepath.Join(root, "sys/class/dmi/id", f))
		s := strings.ToLower(string(b))
		for _, vm := range []string{"virtualbox", "vmware", "qemu", "kvm", "bochs", "parallels", "xen", "hyper-v", "virtual machine", "bhyve"} {
			if strings.Contains(s, vm) {
				return false
			}
		}
	}
	// A render node of a driver with 3D (glprobe.go).
	return gpuDeviceIn(root, gallium)
}
