//go:build windows

// dragprobe is the live drag-out test rig's counterpart: a real Win32
// window registered as an OLE drop target that accepts CF_HDROP, plus a
// synthetic-mouse driver (SendInput) and a window locator, all driven
// over a JSON-line protocol on stdin. The coordinator
// (scripts/drag-live.mjs) floats the app's native drag-out, glides the
// synthetic mouse into this window and releases — everything a drop on
// Explorer does, observable and verifiable:
//
//   - DragEnter / DragOver / DragLeave / Drop arrive (or don't) exactly as
//     they would for Explorer, proving the OLE gesture is alive.
//   - On Drop the CF_HDROP list is read back, every file is statted and
//     MD5-hashed, so the staged bytes are verified end to end.
//
// Protocol (one JSON object per line):
//
//	stdin : {"op":"move","x":N,"y":N}            absolute move (pixels)
//	        {"op":"glide","x":N,"y":N,"steps":N,"ms":N}  interpolated moves
//	        {"op":"down"} / {"op":"up"}          left button ("up" nudges 1px
//	                                         first — see the op's comment)
//	        {"op":"cup"}                        release by posting WM_LBUTTONUP
//	                                         into the drag-tracker window
//	        {"op":"cursor"}                      reply with GetCursorPos
//	        {"op":"screen"}                      reply with SM_C[CX]SCREEN
//	        {"op":"findwin","title":"..."}       locate app window + children
//	        {"op":"winat","x":N,"y":N}           window under a screen point
//	        {"op":"dragout"}                     self-drag control (async)
//	        {"op":"quit"}
//	stdout: {"ev":"ready","hwnd":N,"cx":N,"cy":N}
//	        {"ev":"ack","op":"..."}              command applied
//	        {"ev":"cursor","x":N,"y":N}
//	        {"ev":"screen","w":N,"h":N}
//	        {"ev":"dragout-end","hr":"0x..","effect":N,"paths":[..]}
//	        {"ev":"appwin","left":..,"top":..,"right":..,"bottom":..,
//	         "children":[{"class":"..","left":..,...}]}
//	        {"ev":"enter"} {"ev":"leave"}
//	        {"ev":"over","n":N}                  n = DragOver count so far
//	        {"ev":"drop","paths":[...],"sizes":[...],"missing":[...],
//	         "md5":{"name":"hex"},"effect":N}
//	        {"ev":"leavepull",...}               same payload fields as drop:
//	                                         the leave-time GetData pull
//	        {"ev":"capture","hwnd":N,"class":"..","rect":[..]}
//	                                         the drag-tracker window holding
//	                                         mouse capture, logged on change
//	        {"ev":"qcd-drop","keys":N,"qcd":N,"winat":{..}}
//	                                         the source saw the button release
//	Every event carries "t": milliseconds since probe start.
//
// The window is positioned with -x -y -w -h; the coordinator places it
// beside the app so the two never overlap. All coordinates are physical
// screen pixels; the coordinator converts from the page's CSS pixels
// after comparing screen.width with the "screen" reply (equal ⇒ 100%).
package main

import (
	"bufio"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var le = binary.LittleEndian

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	ole32    = windows.NewLazySystemDLL("ole32.dll")

	pRegisterClassEx  = user32.NewProc("RegisterClassExW")
	pCreateWindowEx   = user32.NewProc("CreateWindowExW")
	pDefWindowProc    = user32.NewProc("DefWindowProcW")
	pGetMessage       = user32.NewProc("GetMessageW")
	pTranslateMessage = user32.NewProc("TranslateMessage")
	pDispatchMessage  = user32.NewProc("DispatchMessageW")
	pPostQuitMessage  = user32.NewProc("PostQuitMessage")
	pPostMessage      = user32.NewProc("PostMessageW")
	pDestroyWindow    = user32.NewProc("DestroyWindow")
	pSendInput        = user32.NewProc("SendInput")
	pGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	pGetCursorPos     = user32.NewProc("GetCursorPos")
	pWindowFromPoint  = user32.NewProc("WindowFromPoint")
	pFindWindowEx     = user32.NewProc("FindWindowExW")
	pGetClassName     = user32.NewProc("GetClassNameW")
	pGetWindowRect    = user32.NewProc("GetWindowRect")

	pEnumWindows     = user32.NewProc("EnumWindows")
	pIsWindowVisible = user32.NewProc("IsWindowVisible")
	pGetWindowText   = user32.NewProc("GetWindowTextW")
	pSetDpiCtx       = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetDpiAware     = user32.NewProc("SetProcessDPIAware")

	pRegisterDragDrop = ole32.NewProc("RegisterDragDrop")
	pOleInitialize    = ole32.NewProc("OleInitialize")
	pOleUninitialize  = ole32.NewProc("OleUninitialize")
	pReleaseStgMedium = ole32.NewProc("ReleaseStgMedium")
	pDoDragDrop       = ole32.NewProc("DoDragDrop")

	pGlobalAlloc    = kernel32.NewProc("GlobalAlloc")
	pGlobalLock     = kernel32.NewProc("GlobalLock")
	pGlobalUnlock   = kernel32.NewProc("GlobalUnlock")
	pGetAsyncKey    = user32.NewProc("GetAsyncKeyState")
	pGetCapture     = user32.NewProc("GetCapture")
	pScreenToClient = user32.NewProc("ScreenToClient")

	pDragQueryFile = shell32.NewProc("DragQueryFileW")
)

const (
	cfHDROP        = 15
	tymedHGlobal   = 1
	dropEffectCopy = 1

	hrOK          = 0
	hrNoInterface = 0x80004002
	hrNotImpl     = 0x80004001
	dvEFormatetc  = 0x80040064

	ddSCancel      = 0x00040100
	ddSDrop        = 0x00040101
	ddSDefaultCrsr = 0x00040102
	mkLButton      = 0x0001
	vkLButton      = 0x01

	gmemMoveable = 0x0002

	wmLButtonUp = 0x0202

	miMove        = 0x0001
	miLeftDown    = 0x0002
	miLeftUp      = 0x0004
	miAbsolute    = 0x8000
	miVirtualDesk = 0x4000

	smCXScreen = 0
	smCYScreen = 1

	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	wmClose = 0x0010

	// wmAppDrag posts the self-drag control onto the pump thread: the drag
	// must run on the STA thread that owns the window and its message loop.
	wmAppDrag = 0x8000 // WM_APP
)

type pointl struct{ X, Y int32 }

type rect struct{ L, T, R, B int32 }

type formatetc struct {
	cfFormat uint16
	ptd      uintptr
	dwAspect uint32
	lindex   int32
	tymed    uint32
}

type stgMedium struct {
	tymed          uint32
	hGlobal        uintptr
	pUnkForRelease uintptr
}

// ---- printing ----

var outMu sync.Mutex

// t0 anchors event timestamps (ms since probe start) for rig forensics.
var t0 = time.Now()

func emit(v any) {
	outMu.Lock()
	defer outMu.Unlock()
	if m, ok := v.(map[string]any); ok {
		m["t"] = time.Since(t0).Milliseconds()
	}
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

// ---- IDropTarget ----

type dropTarget struct {
	vtbl  *dropTargetVtbl
	overN int
	hwnd  uintptr
	// curObj is the data object handed to DragEnter, kept so DragLeave can
	// pull the CF_HDROP payload — the one GetData a real target's Drop
	// would have made. On this machine ole32 never dispatches Drop for a
	// synthetic release (proven against WebView2's own target), so the
	// leave-time pull is the last honest synthetic checkpoint of the
	// app's delay-rendered payload.
	curObj uintptr
}

type dropTargetVtbl struct {
	queryInterface uintptr
	addRef         uintptr
	release        uintptr
	dragEnter      uintptr
	dragOver       uintptr
	dragLeave      uintptr
	drop           uintptr
}

func ptrOf(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

func qI(this, iid, out uintptr) uintptr { // QI: IUnknown only is fine here
	*(*uintptr)(ptrOf(out)) = 0
	return 0x80004002 // E_NOINTERFACE (never asked in practice)
}
func qAddRef(this uintptr) uintptr  { return 2 }
func qRelease(this uintptr) uintptr { return 1 }

func dtDragEnter(this, dataObj, keys, pt, effect uintptr) uintptr {
	// Never interrogate the data object here: the live cross-process drag
	// hands DragEnter a proxy whose vtable probing crashed the probe
	// (execute violation at slot 5). Accept, offer the copy effect; Drop
	// is where the payload gets read — guarded.
	noteCapture("enter")
	if t := (*dropTarget)(ptrOf(this)); t != nil {
		t.curObj = dataObj
	}
	emit(map[string]any{"ev": "enter", "this": this, "obj": dataObj})
	*(*uint32)(ptrOf(effect)) = dropEffectCopy
	return 0
}

func dtDragOver(this, keys, pt, effect uintptr) uintptr {
	t := (*dropTarget)(ptrOf(this))
	t.overN++
	noteCapture("over")
	*(*uint32)(ptrOf(effect)) = dropEffectCopy
	// pt is a POINTL packed in one uintptr: x | y<<32.
	emit(map[string]any{"ev": "over", "n": t.overN, "keys": uint32(keys),
		"px": int32(pt), "py": int32(pt >> 32)})
	return 0
}

func dtDragLeave(this uintptr) uintptr {
	cx, cy := cursorPos()
	obj := uintptr(0)
	if t := (*dropTarget)(ptrOf(this)); t != nil {
		obj, t.curObj = t.curObj, 0
	}
	emit(map[string]any{"ev": "leave", "cx": cx, "cy": cy})
	// The release-time pull: at DragLeave the button is already up, the
	// data object proxy is still alive, and the app's doGetData will now
	// honor the request (it refuses only while the button is held). This
	// is byte-for-byte the GetData a real target's Drop would make.
	if obj != 0 {
		res := pullPayload(obj)
		res["ev"] = "leavepull"
		res["cx"], res["cy"] = cx, cy
		emit(res)
	}
	return 0
}

// pullPayload asks the drag's IDataObject for CF_HDROP, stats and hashes
// every delivered file, and returns the verdict fields. Shared by dtDrop
// (a real Drop, should the machine ever dispatch one) and the leave-time
// pull.
func pullPayload(dataObj uintptr) map[string]any {
	res := map[string]any{}
	// The vtable lives BEHIND the object's first pointer — a COM object IS
	// {vtbl, fields...}; indexing the object itself reads fields as function
	// pointers (the first rig iteration died exactly like that). Load the
	// vtable, then call slot 3 = IDataObject::GetData.
	vtbl := *(**[11]uintptr)(ptrOf(dataObj))
	if vtbl[3] == 0 {
		res["error"] = "GetData vtable slot is null (bad data object proxy)"
		return res
	}
	fe := formatetc{cfFormat: cfHDROP, dwAspect: 1, lindex: -1, tymed: tymedHGlobal}
	var med stgMedium
	hr, _, _ := syscall.SyscallN(vtbl[3], dataObj, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&med))) // GetData
	if hr != 0 {
		res["error"] = fmt.Sprintf("GetData hr=0x%08x", uint32(hr))
		return res
	}
	h := med.hGlobal
	n, _, _ := pDragQueryFile.Call(h, 0xFFFFFFFF, 0, 0)
	paths := []string{}
	sizes := []int64{}
	missing := []string{}
	hashes := map[string]string{}
	for i := uintptr(0); i < n; i++ {
		l, _, _ := pDragQueryFile.Call(h, i, 0, 0)
		if l == 0 {
			continue
		}
		buf := make([]uint16, l+1)
		pDragQueryFile.Call(h, i, uintptr(unsafe.Pointer(&buf[0])), l+1)
		p := windows.UTF16ToString(buf)
		paths = append(paths, p)
		st, err := os.Stat(p)
		if err != nil {
			missing = append(missing, p)
			continue
		}
		sizes = append(sizes, st.Size())
		if st.Size() <= 64<<20 { // hash everything a drag-out may stage
			b, err := os.ReadFile(p)
			if err == nil {
				sum := md5.Sum(b)
				hashes[filepathBase(p)] = hex.EncodeToString(sum[:])
			}
		}
	}
	pReleaseStgMedium.Call(uintptr(unsafe.Pointer(&med)))
	res["paths"] = paths
	res["sizes"] = sizes
	res["missing"] = missing
	res["md5"] = hashes
	return res
}

// capHwnd is the window holding mouse capture while a drag is floating —
// for the self-drag that is ole32's own drag-tracker window, discovered
// from inside the loop's DragEnter/DragOver callbacks (same thread, so
// GetCapture is valid there). The "cup" op releases the drag by posting
// WM_LBUTTONUP straight into it.
var capHwnd uintptr

// noteCapture records the current capture window, emitting one "capture"
// event whenever it changes (per gesture, without a reset hook).
func noteCapture(where string) {
	h, _, _ := pGetCapture.Call()
	if atomic.SwapUintptr(&capHwnd, h) != h {
		ev := map[string]any{"ev": "capture", "where": where, "hwnd": h}
		if h != 0 {
			r := rectOf(h)
			ev["class"] = classOf(h)
			ev["rect"] = []int32{r.L, r.T, r.R, r.B}
			ev["visible"] = visibleOf(h)
		}
		emit(ev)
	}
}

// objHasHDROP is gone: probing the drag's IDataObject (QueryGetData, vtable
// slot 5) crashed the probe on the live cross-process drag — the DragEnter
// proxy did not tolerate it. Drop-time GetData carries the real signal.

// dtDrop reads the CF_HDROP payload, stats + hashes every file, and prints
// the verdict. This is the moment the app's delay-rendered GetData must
// deliver the staged paths.
func dtDrop(this, dataObj, keys, pt, effect uintptr) uintptr {
	emit(map[string]any{"ev": "dropstart", "keys": uint32(keys)})
	*(*uint32)(ptrOf(effect)) = dropEffectCopy
	res := pullPayload(dataObj)
	res["ev"] = "drop"
	emit(res)
	return 0
}

func filepathBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '\\' || p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

var dtVtbl = dropTargetVtbl{
	queryInterface: syscall.NewCallback(qI),
	addRef:         syscall.NewCallback(qAddRef),
	release:        syscall.NewCallback(qRelease),
	dragEnter:      syscall.NewCallback(dtDragEnter),
	dragOver:       syscall.NewCallback(dtDragOver),
	dragLeave:      syscall.NewCallback(dtDragLeave),
	drop:           syscall.NewCallback(dtDrop),
}

// ---- self-drag source (control experiment) ----
//
// The rig asks this process to float its own OLE drag of two REAL temp
// files and drop it onto its own registered window. The probe target, the
// synthetic input and the button-up routing are identical to the app
// tiers — only the drag source differs. If Drop fires here but not for
// the app, the difference lives on the app's source side, not in this rig.

type srcObj struct {
	vtbl  *srcObjVtbl
	paths []string
}

// srcObjVtbl is IDataObject: 3 IUnknown + 8 IDataObject slots, in order.
type srcObjVtbl struct {
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

type dragSrc struct {
	vtbl *dragSrcVtbl
	qcd  int32
}

// dragSrcVtbl is IDropSource: 3 IUnknown + 2 IDropSource slots.
type dragSrcVtbl struct {
	queryInterface    uintptr
	addRef            uintptr
	release           uintptr
	queryContinueDrag uintptr
	giveFeedback      uintptr
}

var (
	iidIUnknown    = windows.GUID{Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIDataObject = windows.GUID{Data1: 0x0000010e, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIDropSource = windows.GUID{
		Data1: 0x4657278b, Data2: 0x411b, Data3: 0x11d2,
		Data4: [8]byte{0x83, 0x9a, 0x00, 0xc0, 0x4f, 0xd9, 0x18, 0xd0},
	}
)

func soQI(this, iid, out uintptr) uintptr {
	if g := (*windows.GUID)(ptrOf(iid)); *g == iidIUnknown || *g == iidIDataObject {
		*(**srcObj)(ptrOf(out)) = (*srcObj)(ptrOf(this))
		return hrOK
	}
	return hrNoInterface
}
func soAddRef(this uintptr) uintptr  { return 2 }
func soRelease(this uintptr) uintptr { return 1 }

// soGetData renders CF_HDROP immediately — these are real files on disk,
// no staging and no button-held refusal (the one app behavior that could
// starve a target).
func soGetData(this, fe, medium uintptr) uintptr {
	f := (*formatetc)(ptrOf(fe))
	if f.cfFormat != cfHDROP || (f.tymed != 0 && f.tymed != tymedHGlobal) {
		return dvEFormatetc
	}
	obj := (*srcObj)(ptrOf(this))
	payload := hdropBytes(obj.paths)
	hMem, _, _ := pGlobalAlloc.Call(gmemMoveable, uintptr(len(payload)))
	if hMem == 0 {
		return dvEFormatetc
	}
	p, _, _ := pGlobalLock.Call(hMem)
	if p == 0 {
		return dvEFormatetc
	}
	copy(unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&p))), len(payload)), payload)
	pGlobalUnlock.Call(hMem)
	m := (*stgMedium)(ptrOf(medium))
	m.tymed = tymedHGlobal
	m.hGlobal = hMem
	m.pUnkForRelease = 0
	return hrOK
}

func soGetDataHere(this, fe, medium uintptr) uintptr        { return hrNotImpl }
func soQueryGetData(this, fe uintptr) uintptr               { return hrOK } // CF_HDROP is offered
func soCanonical(this, in, out uintptr) uintptr             { return hrNotImpl }
func soSetData(this, fe, medium, release uintptr) uintptr   { return hrNotImpl }
func soEnumFormatEtc(this, dir, enum uintptr) uintptr       { return hrNotImpl }
func soDAdvise(this, fe, flags, sink, conn uintptr) uintptr { return hrNotImpl }
func soDUnadvise(this, conn uintptr) uintptr                { return hrNotImpl }

func dsQI(this, iid, out uintptr) uintptr {
	if g := (*windows.GUID)(ptrOf(iid)); *g == iidIUnknown || *g == iidIDropSource {
		*(**dragSrc)(ptrOf(out)) = (*dragSrc)(ptrOf(this))
		return hrOK
	}
	return hrNoInterface
}
func dsAddRef(this uintptr) uintptr  { return 2 }
func dsRelease(this uintptr) uintptr { return 1 }

// dsQCD mirrors the app's end semantics: Escape cancels; a released left
// button ends the gesture as a drop wherever the cursor sits.
func dsQCD(this, esc, keys uintptr) uintptr {
	s := (*dragSrc)(ptrOf(this))
	s.qcd++
	if esc != 0 {
		return ddSCancel
	}
	if keys&mkLButton == 0 {
		// The release instant, seen from the SOURCE side: which window
		// owns the cursor right now — the one ole32's completion routing
		// would resolve to.
		ev := map[string]any{"ev": "qcd-drop", "qcd": s.qcd, "keys": uint32(keys)}
		if w := winUnderCursor(); w != nil {
			ev["winat"] = w
		}
		emit(ev)
		return ddSDrop
	}
	return hrOK
}
func dsGiveFeedback(this, effect uintptr) uintptr { return ddSDefaultCrsr }

// winUnderCursor names the window owning the cursor right now (class +
// rect) — what a fresh hit test resolves at this instant.
func winUnderCursor() map[string]any {
	cx, cy := cursorPos()
	pt := uintptr(uint32(int32(cx))) | uintptr(uint32(int32(cy)))<<32
	hw, _, _ := pWindowFromPoint.Call(pt)
	if hw == 0 {
		return map[string]any{"x": cx, "y": cy, "hwnd": 0}
	}
	r := rectOf(hw)
	return map[string]any{"x": cx, "y": cy, "hwnd": hw, "class": classOf(hw),
		"rect": []int32{r.L, r.T, r.R, r.B}}
}

var (
	srcObjVtblInst = srcObjVtbl{
		queryInterface:        syscall.NewCallback(soQI),
		addRef:                syscall.NewCallback(soAddRef),
		release:               syscall.NewCallback(soRelease),
		getData:               syscall.NewCallback(soGetData),
		getDataHere:           syscall.NewCallback(soGetDataHere),
		queryGetData:          syscall.NewCallback(soQueryGetData),
		getCanonicalFormatEtc: syscall.NewCallback(soCanonical),
		setData:               syscall.NewCallback(soSetData),
		enumFormatEtc:         syscall.NewCallback(soEnumFormatEtc),
		dAdvise:               syscall.NewCallback(soDAdvise),
		dUnadvise:             syscall.NewCallback(soDUnadvise),
	}
	dragSrcVtblInst = dragSrcVtbl{
		queryInterface:    syscall.NewCallback(dsQI),
		addRef:            syscall.NewCallback(dsAddRef),
		release:           syscall.NewCallback(dsRelease),
		queryContinueDrag: syscall.NewCallback(dsQCD),
		giveFeedback:      syscall.NewCallback(dsGiveFeedback),
	}
)

// hdropBytes builds the CF_HDROP payload: DROPFILES header + wide paths,
// each NUL-terminated, the list closed by one extra NUL.
func hdropBytes(paths []string) []byte {
	body := make([]uint16, 0, 64)
	for _, p := range paths {
		body = append(body, utf16.Encode([]rune(p))...)
		body = append(body, 0)
	}
	body = append(body, 0)
	buf := make([]byte, 20+len(body)*2)
	le.PutUint32(buf[0:], 20) // pFiles: byte offset of the path list
	le.PutUint32(buf[16:], 1) // fWide
	for i, u := range body {
		le.PutUint16(buf[20+i*2:], uint16(u))
	}
	return buf
}

// selfDragActive gates wndProc's message logging: while the self-drag's
// modal loop runs, every input/interesting message it dispatches to this
// window is emitted — the release instant becomes fully observable.
var selfDragActive int32

// runSelfDrag floats the control drag on the pump thread (wndProc
// dispatch): DoDragDrop's modal loop pumps messages itself, so the
// command channel stays alive inside the gesture.
func runSelfDrag() {
	dir, err := os.MkdirTemp("", "s3b-probe-src-")
	if err != nil {
		emit(map[string]any{"ev": "dragout-end", "error": err.Error()})
		return
	}
	paths := []string{
		filepath.Join(dir, "probe-one.txt"),
		filepath.Join(dir, "probe-two.txt"),
	}
	os.WriteFile(paths[0], []byte("probe drag file one\n"), 0o644)
	os.WriteFile(paths[1], []byte("probe drag file two\n"), 0o644)
	obj := &srcObj{vtbl: &srcObjVtblInst, paths: paths}
	src := &dragSrc{vtbl: &dragSrcVtblInst}
	var eff uint32
	atomic.StoreInt32(&selfDragActive, 1)
	hr, _, _ := pDoDragDrop.Call(
		uintptr(unsafe.Pointer(obj)), uintptr(unsafe.Pointer(src)),
		dropEffectCopy, uintptr(unsafe.Pointer(&eff)))
	atomic.StoreInt32(&selfDragActive, 0)
	emit(map[string]any{"ev": "dragout-end",
		"hr": fmt.Sprintf("0x%08x", uint32(hr)), "effect": eff, "paths": paths})
	runtime.KeepAlive(obj)
	runtime.KeepAlive(src)
}

// ---- window ----

func wndProc(hwnd, msg, wp, lp uintptr) uintptr {
	if atomic.LoadInt32(&selfDragActive) == 1 {
		// Input + drag-relevant messages only — the point is watching the
		// release instant (the up's lParam carries its own coordinates).
		if (msg >= 0x0200 && msg <= 0x020E) || // WM_MOUSEFIRST..WM_MOUSELAST
			msg == 0x001F || // WM_CANCELMODE
			msg == 0x0215 || // WM_CAPTURECHANGED
			(msg >= 0x0100 && msg <= 0x0108) { // WM_KEYDOWN..WM_SYSKEYUP
			emit(map[string]any{"ev": "msg", "m": msg, "wp": wp,
				"lx": int32(lp), "ly": int32(lp >> 32)})
		}
	}
	switch msg {
	case wmClose:
		pDestroyWindow.Call(hwnd) // we are on the window's own thread here
		return 0
	case 0x0002: // WM_DESTROY
		pPostQuitMessage.Call(0)
		return 0
	case wmAppDrag:
		runSelfDrag() // blocks inside DoDragDrop's own pump until release
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, msg, wp, lp)
	return r
}

var clsName = windows.StringToUTF16Ptr("S3BDragProbe")
var clsNamePtr = uintptr(unsafe.Pointer(clsName))

type wndClassEx struct {
	Size, Style                        uint32
	WndProc                            uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                uintptr
	IconSm                             uintptr
}

// ---- synthetic mouse (SendInput) ----

// mouseInput must mirror Win32 INPUT on amd64 exactly: {DWORD type;
// union{MOUSEINPUT}} where the union is pointer-aligned — 40 bytes total,
// dx/dy at offsets 8/12.
type mouseInput struct {
	Type      uint32
	_         uint32
	Dx, Dy    int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	_         uint32
	ExtraInfo uintptr
}

func virtualNorm(x, y int) (int32, int32) {
	vx, _, _ := pGetSystemMetrics.Call(smXVirtualScreen)
	vy, _, _ := pGetSystemMetrics.Call(smYVirtualScreen)
	vw, _, _ := pGetSystemMetrics.Call(smCXVirtualScreen)
	vh, _, _ := pGetSystemMetrics.Call(smCYVirtualScreen)
	if vw == 0 || vh == 0 {
		return int32(x), int32(y)
	}
	return int32((x - int(vx)) * 65535 / (int(vw) - 1)), int32((y - int(vy)) * 65535 / (int(vh) - 1))
}

func sendMouse(flags uint32, x, y int) {
	in := mouseInput{Type: 0, Flags: flags}
	if flags&miMove != 0 {
		nx, ny := virtualNorm(x, y)
		in.Dx, in.Dy = nx, ny
		in.Flags = flags | miAbsolute | miVirtualDesk
	}
	pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

func cursorPos() (int, int) {
	var pt struct{ X, Y int32 }
	if r, _, _ := pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
		return 0, 0
	}
	return int(pt.X), int(pt.Y)
}

// ---- window discovery ----

type childInfo struct {
	Class                    string `json:"class"`
	Left, Top, Right, Bottom int32
}

func classOf(hwnd uintptr) string {
	buf := make([]uint16, 64)
	n, _, _ := pGetClassName.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 64)
	return windows.UTF16ToString(buf[:n])
}

func textOf(hwnd uintptr) string {
	buf := make([]uint16, 128)
	n, _, _ := pGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 128)
	return windows.UTF16ToString(buf[:n])
}

func visibleOf(hwnd uintptr) bool {
	v, _, _ := pIsWindowVisible.Call(hwnd)
	return v != 0
}

func rectOf(hwnd uintptr) rect {
	var r rect
	pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

// winMatch is one top-level window whose title matches the probe request.
type winMatch struct {
	Hwnd    uintptr `json:"hwnd"`
	Class   string  `json:"class"`
	Visible bool    `json:"visible"`
	Left    int32   `json:"left"`
	Top     int32   `json:"top"`
	Right   int32   `json:"right"`
	Bottom  int32   `json:"bottom"`
}

// findAppWindow enumerates every top-level window with an exact title match
// (several can share a title — conhost relics and shell windows are real
// occurrences), picks the Wails main window (class WailsWebviewWindow; any
// visible match by largest area as fallback), and reports its rect plus two
// levels of children, so the coordinator can map the page's client
// coordinates onto the physical screen. All matches ride along for the
// report — proof of what was rejected.
func findAppWindow(title string) map[string]any {
	matches := []winMatch{}
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if textOf(hwnd) == title {
			r := rectOf(hwnd)
			matches = append(matches, winMatch{
				Hwnd: hwnd, Class: classOf(hwnd), Visible: visibleOf(hwnd),
				Left: r.L, Top: r.T, Right: r.R, Bottom: r.B,
			})
		}
		return 1
	})
	pEnumWindows.Call(cb, 0)
	res := map[string]any{"matches": matches}
	var best *winMatch
	for i := range matches {
		m := &matches[i]
		if best == nil || bestScore(m) > bestScore(best) {
			best = m
		}
	}
	if best == nil {
		res["error"] = "window not found"
		return res
	}
	res["hwnd"] = best.Hwnd
	res["class"] = best.Class
	res["left"], res["top"] = best.Left, best.Top
	res["right"], res["bottom"] = best.Right, best.Bottom
	hw := best.Hwnd
	kids := []childInfo{}
	var prev uintptr
	for {
		ch, _, _ := pFindWindowEx.Call(hw, prev, 0, 0)
		if ch == 0 {
			break
		}
		cr := rectOf(ch)
		kids = append(kids, childInfo{Class: classOf(ch), Left: cr.L, Top: cr.T, Right: cr.R, Bottom: cr.B})
		// one deeper level — the Chromium render widget lives there
		var prev2 uintptr
		for {
			ch2, _, _ := pFindWindowEx.Call(ch, prev2, 0, 0)
			if ch2 == 0 {
				break
			}
			cr2 := rectOf(ch2)
			kids = append(kids, childInfo{Class: classOf(ch2) + "+", Left: cr2.L, Top: cr2.T, Right: cr2.R, Bottom: cr2.B})
			prev2 = ch2
		}
		prev = ch
	}
	res["children"] = kids
	return res
}

// bestScore ranks a match: the Wails main window class wins outright (any
// visible area beats it), then visible windows by area; invisible non-Wails
// windows score zero.
func bestScore(m *winMatch) int32 {
	score := int32(0)
	if m.Class == "WailsWebviewWindow" {
		score += 1 << 30
	}
	if m.Visible {
		score += (m.Right - m.Left) * (m.Bottom - m.Top)
	}
	return score
}

// ---- command loop ----

type cmd struct {
	Op    string `json:"op"`
	X     int    `json:"x,omitempty"`
	Y     int    `json:"y,omitempty"`
	Steps int    `json:"steps,omitempty"`
	Ms    int    `json:"ms,omitempty"`
	Title string `json:"title,omitempty"`
}

func glide(x, y, steps, ms int) {
	cx, cy := cursorPos()
	if steps < 1 {
		steps = 1
	}
	for i := 1; i <= steps; i++ {
		ix := cx + (x-cx)*i/steps
		iy := cy + (y-cy)*i/steps
		sendMouse(miMove, ix, iy)
		if ms > 0 {
			sleepMs(ms / steps)
		}
	}
}

func sleepMs(n int) {
	n = max(n, 1)
	windows.SleepEx(uint32(n), false)
}

var probeHwnd uintptr

func main() {
	// STA discipline: OLE delivers DragEnter/DragOver/Drop to the exact
	// thread that called OleInitialize/RegisterDragDrop, but Go's scheduler
	// migrates goroutines between OS threads freely. Pin the message pump
	// to one thread for the life of the process — the first real OLE
	// gesture crashed the probe without this (callback on the wrong
	// thread, rip=0).
	runtime.LockOSThread()

	// Physical pixels everywhere: an unaware probe would see DPI-virtualized
	// coordinates while the WebView2 page (per-monitor aware) reports CSS px
	// x devicePixelRatio — the mapping math needs the real thing.
	pSetDpiCtx.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	pSetDpiAware.Call()

	x := flag.Int("x", 1200, "window x")
	y := flag.Int("y", 120, "window y")
	w := flag.Int("w", 620, "window width")
	h := flag.Int("h", 460, "window height")
	flag.Parse()

	hi, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)

	wc := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   syscall.NewCallback(wndProc),
		Instance:  hi,
		ClassName: clsNamePtr,
	}
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))

	title := windows.StringToUTF16Ptr("S3B Drag Probe Target")
	// WS_OVERLAPPEDWINDOW(0x00CF0000) | WS_VISIBLE(0x10000000); ex-style
	// WS_EX_TOPMOST(8) | WS_EX_NOACTIVATE(0x08000000): the target never
	// hides mid-gesture and never steals focus from the app — a real
	// dragstart in the webview needs the app window in the foreground.
	hwnd, _, _ := pCreateWindowEx.Call(8|0x08000000, clsNamePtr, uintptr(unsafe.Pointer(title)),
		0x00CF0000|0x10000000, uintptr(*x), uintptr(*y), uintptr(*w), uintptr(*h),
		0, 0, hi, 0)
	if hwnd == 0 {
		emit(map[string]any{"ev": "fatal", "error": "CreateWindow failed"})
		os.Exit(1)
	}
	probeHwnd = hwnd

	if hr, _, _ := pOleInitialize.Call(0); int32(hr) < 0 {
		emit(map[string]any{"ev": "fatal", "error": fmt.Sprintf("OleInitialize hr=0x%08x", uint32(hr))})
		os.Exit(1)
	}
	defer pOleUninitialize.Call()

	target := &dropTarget{vtbl: &dtVtbl, hwnd: hwnd}
	if hr, _, _ := pRegisterDragDrop.Call(hwnd, uintptr(unsafe.Pointer(target))); int32(hr) < 0 {
		emit(map[string]any{"ev": "fatal", "error": fmt.Sprintf("RegisterDragDrop hr=0x%08x", uint32(hr))})
		os.Exit(1)
	}
	emit(map[string]any{"ev": "ready", "hwnd": hwnd, "cx": *x + *w/2, "cy": *y + *h/2})

	// command reader: SendInput and the discovery APIs are callable from
	// any thread, so the loop stays off the STA message pump. quit must
	// not call DestroyWindow cross-thread — it posts WM_CLOSE instead.
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 64<<10), 64<<10)
		for sc.Scan() {
			var c cmd
			if json.Unmarshal(sc.Bytes(), &c) != nil || c.Op == "" {
				continue
			}
			switch c.Op {
			case "move":
				sendMouse(miMove, c.X, c.Y)
			case "glide":
				glide(c.X, c.Y, c.Steps, c.Ms)
			case "down":
				sendMouse(miLeftDown, 0, 0)
			case "up":
				// The release must carry a TRUE mouse point: ole32's drag
				// loop re-hit-tests msg.pt of every message it dispatches,
				// and an injected button-up alone can carry a point that
				// resolves off-target — the loop then swaps targets at the
				// release instant (DragLeave, effect 0, no Drop; seen live
				// in the self-drag control). A real 1px move first lands a
				// WM_MOUSEMOVE with a genuine point in the tracker, and the
				// move+up that follows releases on that same point. A
				// move-to-the-identical-position can coalesce to nothing,
				// hence the nudge.
				cx, cy := cursorPos()
				sendMouse(miMove, cx+1, cy)
				sleepMs(20)
				sendMouse(miMove|miLeftUp, cx+1, cy)
			case "cup":
				// Release the drag by posting WM_LBUTTONUP directly into the
				// drag-tracker window (the hidden ole32 window that holds
				// capture during DoDragDrop), with the release point in ITS
				// client coordinates. Injected SendInput button-ups ended
				// every gesture with DragLeave + effect 0 even when the
				// tracker demonstrably processed a true move at the correct
				// point; feeding the loop's own window the release message
				// bypasses the input-injection pipeline entirely. Falls back
				// to a plain injected up when no capture window is known
				// (e.g. the drag is floated by another process, whose
				// capture GetCapture cannot see).
				h := atomic.LoadUintptr(&capHwnd)
				if h == 0 {
					cx, cy := cursorPos()
					sendMouse(miLeftUp, 0, 0)
					emit(map[string]any{"ev": "cup", "mode": "sendinput", "cx": cx, "cy": cy})
				} else {
					cx, cy := cursorPos()
					pt := struct{ X, Y int32 }{int32(cx), int32(cy)}
					pScreenToClient.Call(h, uintptr(unsafe.Pointer(&pt)))
					lp := uintptr(uint32(pt.X)) | uintptr(uint32(pt.Y))<<32
					r, _, _ := pPostMessage.Call(h, wmLButtonUp, 0, lp)
					emit(map[string]any{"ev": "cup", "mode": "posted", "hwnd": h,
						"cx": cx, "cy": cy, "lx": pt.X, "ly": pt.Y, "ok": r != 0})
				}
			case "cursor":
				cx, cy := cursorPos()
				emit(map[string]any{"ev": "cursor", "x": cx, "y": cy})
				continue
			case "screen":
				sw, _, _ := pGetSystemMetrics.Call(smCXScreen)
				sh, _, _ := pGetSystemMetrics.Call(smCYScreen)
				emit(map[string]any{"ev": "screen", "w": sw, "h": sh})
				continue
			case "findwin":
				emit(map[string]any{"ev": "appwin", "data": findAppWindow(c.Title)})
				continue
			case "winat":
				// Which window owns the drop point? The rig asks right
				// before releasing — what OLE's drop routing will see.
				// POINT is passed BY VALUE — one packed 8-byte argument
				// (x | y<<32), not two separate ints.
				pt := uintptr(uint32(int32(c.X))) | uintptr(uint32(int32(c.Y)))<<32
				hw, _, _ := pWindowFromPoint.Call(pt)
				ans := map[string]any{"ev": "winat", "x": c.X, "y": c.Y}
				if hw != 0 {
					r := rectOf(hw)
					ans["hwnd"] = hw
					ans["class"] = classOf(hw)
					ans["rect"] = []int32{r.L, r.T, r.R, r.B}
					ans["visible"] = visibleOf(hw)
				}
				emit(ans)
				continue
			case "dragout":
				// Self-drag control: posted to the pump thread; the generic
				// ack below means "posted" — dragout-end reports the outcome.
				pPostMessage.Call(probeHwnd, wmAppDrag, 0, 0)
			case "quit":
				pPostMessage.Call(probeHwnd, wmClose, 0, 0)
				return
			}
			emit(map[string]any{"ev": "ack", "op": c.Op})
		}
	}()

	// MSG on x64 is 48 bytes (hwnd, msg, wp, lp, time, pt, lPrivate).
	msg := (*struct {
		Hwnd   uintptr
		Msg    uint32
		Wp, Lp uintptr
		Time   uint32
		Pt     pointl
		LPriv  uint32
	})(unsafe.Pointer(&[48]byte{}))
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(msg)), 0, 0, 0)
		if r == 0 || int32(r) == -1 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(msg)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(msg)))
	}
}
