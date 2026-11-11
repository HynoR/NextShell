//go:build windows && (amd64 || arm64)

package windows

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

// The recycle bin, through IFileOperation as Electron's shell.trashItem.
// Windows deletes an item for good when it cannot recycle it: one too big
// for the recycle bin, or on a drive without one, as network shares and
// many removable drives are. Without a UI nothing asks first, so a
// progress sink refuses those deletes and TrashItem fails instead.

var (
	clsidFileOperation            = guid("3ad05575-8857-4850-9277-11b85bdb8e09")
	iidIFileOperation             = guid("947aab5f-0a5c-4c13-b4d6-4bf7836fc9f8")
	iidIFileOperationProgressSink = guid("04b0f1a7-9490-44bc-96e1-4296a31252e2")
	trashSinkVtbl                 [19]uintptr
	trashSinkOnce                 sync.Once
	trashSinks                    = map[uintptr]*trashSink{}
)

const (
	fofSilent                  = 0x0004
	fofNoConfirmation          = 0x0010
	fofAllowUndo               = 0x0040
	fofNoConfirmMkdir          = 0x0200
	fofNoErrorUI               = 0x0400
	fofNoUI                    = fofSilent | fofNoConfirmation | fofNoErrorUI | fofNoConfirmMkdir
	fofxRecycleOnDelete        = 0x00080000
	fofxAddUndoRecord          = 0x20000000
	tsfDeleteRecycleIfPossible = 0x80
	eAbort                     = 0x80004004

	// IFileOperation methods.
	foSetOperationFlags       = 5
	foDeleteItem              = 18
	foPerformOperations       = 21
	foGetAnyOperationsAborted = 22
)

// A trashSink is an IFileOperationProgressSink: only PreDeleteItem does
// anything, the other methods succeed.
type trashSink struct {
	vtbl    *[19]uintptr
	refs    int32
	refused bool // a delete would not have gone to the recycle bin
}

func initTrashSink() {
	cb := syscall.NewCallback
	ok := cb(func(this uintptr) uintptr { return sOK })
	ok3 := cb(func(this, a, b, c uintptr) uintptr { return sOK })
	ok5 := cb(func(this, a, b, c, d, e uintptr) uintptr { return sOK })
	ok6 := cb(func(this, a, b, c, d, e, f uintptr) uintptr { return sOK })
	trashSinkVtbl = [19]uintptr{
		cb(func(this, riid, out uintptr) uintptr {
			*(*uintptr)(native(out)) = 0
			if iid := *(*GUID)(native(riid)); iid != iidIUnknown && iid != iidIFileOperationProgressSink {
				return eNoInterface
			}
			*(*uintptr)(native(out)) = this
			trashSinks[this].refs++
			return sOK
		}),
		cb(func(this uintptr) uintptr { s := trashSinks[this]; s.refs++; return uintptr(s.refs) }),
		cb(func(this uintptr) uintptr { s := trashSinks[this]; s.refs--; return uintptr(s.refs) }),
		ok, // StartOperations
		cb(func(this, hr uintptr) uintptr { return sOK }), // FinishOperations
		ok3, // PreRenameItem
		ok5, // PostRenameItem
		cb(func(this, a, b, c, d uintptr) uintptr { return sOK }), // PreMoveItem
		ok6, // PostMoveItem
		cb(func(this, a, b, c, d uintptr) uintptr { return sOK }), // PreCopyItem
		ok6, // PostCopyItem
		// PreDeleteItem(flags, item)
		cb(func(this, flags, item uintptr) uintptr {
			if uint32(flags)&tsfDeleteRecycleIfPossible == 0 {
				trashSinks[this].refused = true
				return eAbort
			}
			return sOK
		}),
		cb(func(this, a, b, c, d uintptr) uintptr { return sOK }), // PostDeleteItem
		ok3, // PreNewItem
		cb(func(this, a, b, c, d, e, f, g uintptr) uintptr { return sOK }), // PostNewItem
		cb(func(this, total, done uintptr) uintptr { return sOK }),         // UpdateProgress
		ok, ok, ok, // ResetTimer, PauseTimer, ResumeTimer
	}
}

func (shell) TrashItem(path string) error {
	trashSinkOnce.Do(initTrashSink)
	var item uintptr
	if r, _, _ := procSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(u16(path))), 0, uintptr(unsafe.Pointer(&iidIShellItem)), uintptr(unsafe.Pointer(&item))); failed(r) || item == 0 {
		return fmt.Errorf("mygo: cannot move %s to the recycle bin: %w", path, hresultError("SHCreateItemFromParsingName", r))
	}
	defer release(item)
	// The operation holds the sink until it is released, which the
	// deferred release below does before the sink leaves the map.
	sink := &trashSink{vtbl: &trashSinkVtbl, refs: 1}
	p := uintptr(unsafe.Pointer(sink))
	trashSinks[p] = sink
	defer delete(trashSinks, p)
	var op uintptr
	if r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidFileOperation)), 0, clsctxInprocServer, uintptr(unsafe.Pointer(&iidIFileOperation)), uintptr(unsafe.Pointer(&op))); failed(r) || op == 0 {
		return hresultError("CoCreateInstance(FileOperation)", r)
	}
	defer release(op)
	if r := comCall(op, foSetOperationFlags, fofNoUI|fofAllowUndo|fofxRecycleOnDelete|fofxAddUndoRecord); failed(r) {
		return hresultError("IFileOperation.SetOperationFlags", r)
	}
	if r := comCall(op, foDeleteItem, item, p); failed(r) {
		return hresultError("IFileOperation.DeleteItem", r)
	}
	r := comCall(op, foPerformOperations)
	var aborted int32
	comCall(op, foGetAnyOperationsAborted, uintptr(unsafe.Pointer(&aborted)))
	switch {
	case sink.refused:
		return fmt.Errorf("mygo: cannot move %s to the recycle bin: it would be deleted permanently", path)
	case failed(r):
		return fmt.Errorf("mygo: cannot move %s to the recycle bin: %w", path, hresultError("IFileOperation.PerformOperations", r))
	case aborted != 0:
		return fmt.Errorf("mygo: cannot move %s to the recycle bin", path)
	}
	return nil
}
