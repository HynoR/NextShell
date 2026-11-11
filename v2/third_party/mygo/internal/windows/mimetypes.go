//go:build windows && (amd64 || arm64)

package windows

import (
	"strings"
	"syscall"
	"unsafe"
)

// hkeyClassesRoot holds the extensions of files and their types.
const hkeyClassesRoot = 0x80000000

// mimeTypes are the extensions of files the registry knows, with their
// MIME types (Content Type) and kinds (PerceivedType), as file dialogs
// filter by type (FileFilter.MIMETypes).
type mimeTypes struct {
	exts []mimeExtension
}

type mimeExtension struct {
	ext, contentType, perceived string
}

// readMIMETypes reads the extensions of HKEY_CLASSES_ROOT that have a
// type or a kind: hundreds of keys, read as a dialog opens.
func readMIMETypes() *mimeTypes {
	t := &mimeTypes{}
	var name [256]uint16
	for i := uint32(0); ; i++ {
		n := uint32(len(name))
		if r, _, _ := procRegEnumKeyExW.Call(hkeyClassesRoot, uintptr(i), uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&n)), 0, 0, 0, 0); r != 0 {
			if syscall.Errno(r) == syscall.ERROR_MORE_DATA {
				continue // a name too long for an extension
			}
			break // ERROR_NO_MORE_ITEMS
		}
		if n < 2 || name[0] != '.' {
			continue
		}
		ext := syscall.UTF16ToString(name[:n])
		var key uintptr
		if r, _, _ := procRegOpenKeyExW.Call(hkeyClassesRoot, uintptr(unsafe.Pointer(&name[0])), 0, keyQueryValue, uintptr(unsafe.Pointer(&key))); r != 0 {
			continue
		}
		e := mimeExtension{ext: strings.ToLower(ext), contentType: strings.ToLower(regValue(key, "Content Type")), perceived: strings.ToLower(regValue(key, "PerceivedType"))}
		procRegCloseKey.Call(key)
		if e.contentType != "" || e.perceived != "" {
			t.exts = append(t.exts, e)
		}
	}
	return t
}

// regValue reads the string value name of an open key, "" if it has none.
func regValue(key uintptr, name string) string {
	var buf [128]uint16
	size := uint32(len(buf) * 2)
	var kind uint32
	if r, _, _ := procRegQueryValueExW.Call(key, uintptr(unsafe.Pointer(u16(name))), 0, uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r != 0 || kind != regSz {
		return ""
	}
	return syscall.UTF16ToString(buf[:size/2])
}

// extensions returns the extensions, with their dots, of the MIME type m,
// or of every type of its kind for "kind/*": those whose type is of the
// kind, or whose PerceivedType is (image, audio, video, text). "*/*"
// is every file's, "*".
func (t *mimeTypes) extensions(m string) []string {
	m = strings.ToLower(strings.TrimSpace(m))
	if m == "*/*" || m == "*" {
		return []string{".*"}
	}
	kind, wild := strings.CutSuffix(m, "/*")
	var out []string
	for _, e := range t.exts {
		if wild && (strings.HasPrefix(e.contentType, kind+"/") || e.perceived == kind) || !wild && e.contentType == m {
			out = append(out, e.ext)
		}
	}
	return out
}
