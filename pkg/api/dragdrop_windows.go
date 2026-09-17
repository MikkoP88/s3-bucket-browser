//go:build windows

package api

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Native drag-out, Windows side: a dedicated STA thread floats the OLE drag
// (DoDragDrop) with a hand-rolled IDataObject offering CF_HDROP and an
// IDropSource. Everything the drop target sees is delay-rendered from the
// staging download, so the gesture starts the moment the user drags and a
// drop on Explorer blocks until the real files exist — then copies them.
// This mirrors how Electron implements webContents.startDrag: the host, not
// the webview, owns drags that must leave the window.
//
// The same gesture serves INTERNAL drops: released over the app's own
// window it is reported as drag:self-drop (client coordinates + release
// modifiers) and the frontend routes it through the ordinary move/copy
// logic — no staging, no import. That is what makes an unmodified drag
// possible: WebView2 cannot convert a running DOM drag into a file drag,
// so the native drag must own every ending.

var (
	ole32dll    = windows.NewLazySystemDLL("ole32.dll")
	pOleInit    = ole32dll.NewProc("OleInitialize")
	pOleUninit  = ole32dll.NewProc("OleUninitialize")
	pDoDragDrop = ole32dll.NewProc("DoDragDrop")
	user32dll   = windows.NewLazySystemDLL("user32.dll")
	pGetCursor  = user32dll.NewProc("GetCursorPos")
)

// HRESULTs and OLE constants (winerror.h).
const (
	hrOK                       = 0
	hrNoInterface              = 0x80004002
	hrNotImpl                  = 0x80004001
	dvEFormatetc               = 0x80040064
	dragdropSDrop              = 0x00040100
	dragdropSCancel            = 0x00040101
	dragdropSUseDefaultCursors = 0x00040102
	mkLButton                  = 0x0001
	mkShift                    = 0x0004
	mkControl                  = 0x0008
	dropEffectNone             = 0
	dropEffectCopy             = 1
	dropEffectLink             = 4
	tymedHGlobal               = 1
	dvaspectContent            = 1
)

// IIDs the drag objects answer QueryInterface with.
var (
	iidIUnknown    = windows.GUID{Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIDataObject = windows.GUID{Data1: 0x0000010e, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIDropSource = windows.GUID{
		Data1: 0x4657278b, Data2: 0x411b, Data3: 0x11d2,
		Data4: [8]byte{0x83, 0x9a, 0x00, 0xc0, 0x4f, 0xd9, 0x18, 0xd0},
	}
)

// formatEtc / stgMedium mirror the OLE structures (pkg/api stays
// framework-free, so the wails w32 copies are off limits).
type formatEtc struct {
	cfFormat uint16
	ptd      uintptr // DVTARGETDEVICE* — always null here
	dwAspect uint32
	lindex   int32
	tymed    uint32
}

type stgMedium struct {
	tymed          uint32
	hGlobal        uintptr // union; TYMED_HGLOBAL carries the HGLOBAL
	pUnkForRelease uintptr
}

// dragDataObject is the COM IDataObject of one drag: everything except
// CF_HDROP is refused; CF_HDROP is delay-rendered from the staging
// download (the payload is allocated per GetData call and owned by the
// callee until ReleaseStgMedium).
type dragDataObject struct {
	vtbl  *dataObjectVtbl
	stage *dragStage
	refs  int32
}

type dataObjectVtbl struct {
	queryInterface        uintptr
	addRef                uintptr
	release               uintptr
	getData               uintptr
	getDataHere           uintptr
	queryGetData          uintptr
	getCanonicalFormatEtc uintptr
	setData               uintptr
	enumFormatEtc         uintptr
	dAdvise               uintptr
	dUnadvise             uintptr
}

// dragDropSource is the COM IDropSource: it ends the gesture when the
// button comes up (drop) or Escape is pressed (cancel) and lets OLE paint
// the standard drag cursors.
type dragDropSource struct {
	vtbl *dropSourceVtbl
	refs int32
}

type dropSourceVtbl struct {
	queryInterface    uintptr
	addRef            uintptr
	release           uintptr
	queryContinueDrag uintptr
	giveFeedback      uintptr
}

// comPtr loads a COM argument pointer: the bits arrive as uintptr (the
// callback boundary has no pointer type), and the double dereference is
// the same vet-safe workaround clipboard_windows.go uses for Call results.
func comPtr(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

// The callback arg counts must match the COM method signatures exactly —
// the caller cleans the stack per the contract, and a mismatch corrupts it.

func doQueryInterface(this, iid, out uintptr) uintptr {
	if g := (*windows.GUID)(comPtr(iid)); *g == iidIUnknown || *g == iidIDataObject {
		obj := (*dragDataObject)(comPtr(this))
		atomic.AddInt32(&obj.refs, 1)
		*(**dragDataObject)(comPtr(out)) = obj
		return hrOK
	}
	*(**dragDataObject)(comPtr(out)) = nil
	return hrNoInterface
}

func doAddRef(this uintptr) uintptr {
	obj := (*dragDataObject)(comPtr(this))
	return uintptr(atomic.AddInt32(&obj.refs, 1))
}

func doRelease(this uintptr) uintptr {
	// The drag goroutine keeps the object alive through DoDragDrop; the
	// refcount only honors COM etiquette, it never frees.
	obj := (*dragDataObject)(comPtr(this))
	return uintptr(atomic.AddInt32(&obj.refs, -1))
}

func doGetData(this, fe, medium uintptr) uintptr {
	obj := (*dragDataObject)(comPtr(this))
	if !feMatchesHDROP((*formatEtc)(comPtr(fe))) {
		return dvEFormatetc
	}
	// Self-drop: the drag is landing on our own window. An empty file list
	// keeps the webview's import path inert and — crucially — stages not a
	// single byte; the internal routing rides the drag:self-drop event,
	// which carries coordinates and modifiers, never paths.
	if _, _, over := dragOverSelf(); over {
		return fillMedium(medium, nil)
	}
	if obj.stage == nil || obj.stage.wait() != nil {
		return dvEFormatetc // staging failed or timed out — refuse the drop
	}
	return fillMedium(medium, obj.stage.paths)
}

// fillMedium allocates the CF_HDROP HGLOBAL for paths (possibly none) and
// hands it to the callee through medium.
func fillMedium(medium uintptr, paths []string) uintptr {
	hMem, err := allocHDROP(paths)
	if err != nil {
		return dvEFormatetc
	}
	m := (*stgMedium)(comPtr(medium))
	m.tymed = tymedHGlobal
	m.hGlobal = hMem
	m.pUnkForRelease = 0
	return hrOK
}

func doGetDataHere(this, fe, medium uintptr) uintptr { return hrNotImpl }

func doQueryGetData(this, fe uintptr) uintptr {
	if feMatchesHDROP((*formatEtc)(comPtr(fe))) {
		return hrOK // promised delay-rendered; the cursor may show copy
	}
	return dvEFormatetc
}

func doGetCanonicalFormatEtc(this, in, out uintptr) uintptr { return hrNotImpl }
func doSetData(this, fe, medium, release uintptr) uintptr   { return hrNotImpl }
func doEnumFormatEtc(this, dir, enum uintptr) uintptr       { return hrNotImpl }
func doDAdvise(this, fe, flags, sink, conn uintptr) uintptr { return hrNotImpl }
func doDUnadvise(this, conn uintptr) uintptr                { return hrNotImpl }

// feMatchesHDROP reports whether fe asks for the one format offered.
func feMatchesHDROP(f *formatEtc) bool {
	if f.cfFormat != cfHDROP {
		return false
	}
	if f.tymed != 0 && f.tymed != tymedHGlobal { // 0 = any medium
		return false
	}
	return f.dwAspect == 0 || f.dwAspect == dvaspectContent
}

func dsQueryInterface(this, iid, out uintptr) uintptr {
	if g := (*windows.GUID)(comPtr(iid)); *g == iidIUnknown || *g == iidIDropSource {
		obj := (*dragDropSource)(comPtr(this))
		atomic.AddInt32(&obj.refs, 1)
		*(**dragDropSource)(comPtr(out)) = obj
		return hrOK
	}
	*(**dragDropSource)(comPtr(out)) = nil
	return hrNoInterface
}

func dsAddRef(this uintptr) uintptr {
	return uintptr(atomic.AddInt32(&(*dragDropSource)(comPtr(this)).refs, 1))
}

func dsRelease(this uintptr) uintptr {
	return uintptr(atomic.AddInt32(&(*dragDropSource)(comPtr(this)).refs, -1))
}

// dragOutKeys holds the last grfKeyState QueryContinueDrag saw — read after
// DoDragDrop returns, it carries the modifier keys held at release.
var dragOutKeys atomic.Uint32

// dsQueryContinueDrag ends the gesture: Escape cancels, a released left
// button drops wherever the cursor sits.
func dsQueryContinueDrag(this, esc, keys uintptr) uintptr {
	dragOutKeys.Store(uint32(keys))
	if esc != 0 {
		return dragdropSCancel
	}
	if keys&mkLButton == 0 {
		return dragdropSDrop
	}
	return hrOK
}

func dsGiveFeedback(this, effect uintptr) uintptr { return dragdropSUseDefaultCursors }

// Vtables are built once: syscall.NewCallback slots are a scarce,
// never-freed resource (same rationale as the w32 package).
var (
	dragDataObjectVtbl = dataObjectVtbl{
		queryInterface:        syscall.NewCallback(doQueryInterface),
		addRef:                syscall.NewCallback(doAddRef),
		release:               syscall.NewCallback(doRelease),
		getData:               syscall.NewCallback(doGetData),
		getDataHere:           syscall.NewCallback(doGetDataHere),
		queryGetData:          syscall.NewCallback(doQueryGetData),
		getCanonicalFormatEtc: syscall.NewCallback(doGetCanonicalFormatEtc),
		setData:               syscall.NewCallback(doSetData),
		enumFormatEtc:         syscall.NewCallback(doEnumFormatEtc),
		dAdvise:               syscall.NewCallback(doDAdvise),
		dUnadvise:             syscall.NewCallback(doDUnadvise),
	}
	dragDropSourceVtbl = dropSourceVtbl{
		queryInterface:    syscall.NewCallback(dsQueryInterface),
		addRef:            syscall.NewCallback(dsAddRef),
		release:           syscall.NewCallback(dsRelease),
		queryContinueDrag: syscall.NewCallback(dsQueryContinueDrag),
		giveFeedback:      syscall.NewCallback(dsGiveFeedback),
	}
)

// dragOutMu serializes gestures — DoDragDrop is a modal loop capturing the
// mouse, and two of them would fight over it.
var dragOutMu sync.Mutex

// hrFailed reports a failing HRESULT (bit 31 set).
func hrFailed(hr uintptr) bool { return hr&0x80000000 != 0 }

// dragOverSelf reports the cursor as webview client coordinates plus
// whether it sits over the app's own window (gui.go installs the mapping;
// without a shell the answer is always "not over self").
func dragOverSelf() (int, int, bool) {
	if shell == nil || shell.ScreenToClient == nil {
		return 0, 0, false
	}
	var pt struct{ X, Y int32 }
	if r, _, _ := pGetCursor.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
		return 0, 0, false
	}
	return shell.ScreenToClient(int(pt.X), int(pt.Y))
}

// dragOutRun stages the selection and floats the native drag. It blocks
// until the gesture ends; the staging download keeps running while the OLE
// loop owns the mouse and is cancelled when the drag is abandoned.
func (a *App) dragOutRun(items []DragItem) error {
	if !dragOutMu.TryLock() {
		return errDragOutBusy
	}
	defer dragOutMu.Unlock()
	dragOutGesture.Store(true)
	defer dragOutGesture.Store(false)

	done := make(chan error, 1)
	go func() {
		// DoDragDrop wants an STA thread; a fresh locked OS thread with
		// OleInitialize is exactly that and leaves the app's threads alone.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if hr, _, _ := pOleInit.Call(0); hrFailed(hr) {
			done <- fmt.Errorf("OleInitialize failed (0x%08x)", uint32(hr))
			return
		}
		defer pOleUninit.Call()

		st := a.startDragStage(items)
		obj := &dragDataObject{vtbl: &dragDataObjectVtbl, stage: st}
		src := &dragDropSource{vtbl: &dragDropSourceVtbl}
		var eff uint32
		hr, _, _ := pDoDragDrop.Call(
			uintptr(unsafe.Pointer(obj)),
			uintptr(unsafe.Pointer(src)),
			dropEffectCopy|dropEffectLink, // a drag-out copies (or links); never a move
			uintptr(unsafe.Pointer(&eff)),
		)
		// DRAGDROP_S_DROP / _CANCEL are success endings of the gesture;
		// either way the mouse is back — stop staging nobody will drop.
		st.cancel()
		// A non-cancelled gesture released over our own window is an
		// internal drop: hand the frontend the coordinates and the
		// release-time modifiers and let the ordinary move/copy logic run.
		// Emitted before DragOutFiles resolves so the frontend's gesture
		// flag is still up when the event lands.
		if hr != dragdropSCancel && !hrFailed(hr) {
			if cx, cy, over := dragOverSelf(); over {
				keys := dragOutKeys.Load()
				if shell != nil && shell.Emit != nil {
					shell.Emit(EventDragSelfDrop, cx, cy, keys&mkShift != 0, keys&mkControl != 0)
				}
			}
		}
		runtime.KeepAlive(obj)
		runtime.KeepAlive(src)
		done <- nil
	}()
	return <-done
}

// allocHDROP builds the CF_HDROP HGLOBAL for paths; ownership passes to the
// OLE callee (Explorer frees it through ReleaseStgMedium).
func allocHDROP(paths []string) (uintptr, error) {
	payload := hdropBytes(paths)
	hMem, _, _ := pGAlloc.Call(gmemMoveable, uintptr(len(payload)))
	if hMem == 0 {
		return 0, syscall.GetLastError()
	}
	ptr, _, _ := pGLock.Call(hMem)
	if ptr == 0 {
		pGFree.Call(hMem)
		return 0, syscall.GetLastError()
	}
	// LazyProc.Call is not recognized as a syscall boundary by vet, so the
	// pointer rides through a dereference (unsafe rule 3 workaround).
	buf := unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&ptr))), len(payload))
	copy(buf, payload)
	pGUnlock.Call(hMem)
	return hMem, nil
}
