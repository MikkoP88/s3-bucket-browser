//go:build windows

package api

import (
	"errors"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const cfHDROP = 15 // CF_HDROP: drag-drop file list

var (
	user32    = windows.NewLazySystemDLL("user32.dll")
	shell32   = windows.NewLazySystemDLL("shell32.dll")
	kernel32c = windows.NewLazySystemDLL("kernel32.dll")
	pOpenClp  = user32.NewProc("OpenClipboard")
	pCloseCl  = user32.NewProc("CloseClipboard")
	pEmptyCl  = user32.NewProc("EmptyClipboard")
	pIsAvail  = user32.NewProc("IsClipboardFormatAvailable")
	pGetData  = user32.NewProc("GetClipboardData")
	pSetData  = user32.NewProc("SetClipboardData")
	pGetSeq   = user32.NewProc("GetClipboardSequenceNumber")
	pDQFile   = shell32.NewProc("DragQueryFileW")
	pGAlloc   = kernel32c.NewProc("GlobalAlloc")
	pGLock    = kernel32c.NewProc("GlobalLock")
	pGUnlock  = kernel32c.NewProc("GlobalUnlock")
	pGFree    = kernel32c.NewProc("GlobalFree")
)

const gmemMoveable = 0x0002

// osClipboardSeq returns the system clipboard sequence number — bumped on
// EVERY clipboard write by any process. Cheap (no OpenClipboard), so the
// frontend can poll it to detect "the user copied something elsewhere since
// our last mirror" and give the OS payload paste precedence.
func osClipboardSeq() uint64 {
	ret, _, _ := pGetSeq.Call()
	return uint64(ret)
}

// osClipboardHasFiles reports whether the clipboard currently holds a
// CF_HDROP file list, without opening it.
func osClipboardHasFiles() bool {
	ret, _, _ := pIsAvail.Call(cfHDROP)
	return ret != 0
}

// osClipboardFiles reads the CF_HDROP list from the clipboard.
func osClipboardFiles() []string {
	out := []string{}
	// The clipboard is a shared resource another process may hold open
	// mid-write; a brief retry turns a transient failure into a success.
	for attempt := 0; attempt < 10; attempt++ {
		if ret, _, _ := pOpenClp.Call(0); ret != 0 {
			defer pCloseCl.Call()
			if ret, _, _ := pIsAvail.Call(cfHDROP); ret == 0 {
				return out
			}
			h, _, _ := pGetData.Call(cfHDROP)
			if h == 0 {
				return out
			}
			// DragQueryFileW(0xFFFFFFFF) returns the file count.
			n, _, _ := pDQFile.Call(h, 0xFFFFFFFF, 0, 0)
			for i := uintptr(0); i < n; i++ {
				l, _, _ := pDQFile.Call(h, i, 0, 0)
				if l == 0 {
					continue
				}
				buf := make([]uint16, l+1)
				if ret, _, _ := pDQFile.Call(h, i, uintptr(unsafe.Pointer(&buf[0])), l+1); ret == 0 {
					continue
				}
				out = append(out, windows.UTF16ToString(buf))
			}
			return out
		}
		time.Sleep(20 * time.Millisecond)
	}
	return out
}

// utf16ListLen: runes of s + NUL, as UTF-16 units.
func utf16Units(s string) int {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return len([]rune(s)) + 1
	}
	return len(u)
}

func utf16Copy(dst []byte, s string) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		u = utf16.Encode([]rune(s))
		u = append(u, 0)
	}
	for i, c := range u {
		*(*uint16)(unsafe.Pointer(&dst[i*2])) = c
	}
}

// hdropBytes lays out a CF_HDROP payload: DROPFILES (20 bytes, fWide) +
// double-null-terminated UTF-16 path list. Shared by the OS clipboard
// write and the native drag-out data object.
func hdropBytes(paths []string) []byte {
	units := 1 // trailing list-terminating NUL
	for _, p := range paths {
		units += utf16Units(p)
	}
	buf := make([]byte, 20+units*2)
	// DROPFILES: pFiles=20 (payload offset), fWide=TRUE.
	*(*uint32)(unsafe.Pointer(&buf[0])) = 20
	*(*uint32)(unsafe.Pointer(&buf[16])) = 1
	off := 20
	for _, p := range paths {
		n := utf16Units(p) * 2
		utf16Copy(buf[off:off+n], p)
		off += n
	}
	// The final list-terminating NUL is already zeroed.
	return buf
}

// osClipboardSetFiles writes a CF_HDROP list onto the clipboard. The system
// owns the global memory after SetClipboardData succeeds.
func osClipboardSetFiles(paths []string) error {
	if len(paths) == 0 {
		return errors.New("no paths given")
	}
	payload := hdropBytes(paths)
	hMem, _, _ := pGAlloc.Call(gmemMoveable, uintptr(len(payload)))
	if hMem == 0 {
		return syscall.GetLastError()
	}
	ptr, _, _ := pGLock.Call(hMem)
	if ptr == 0 {
		pGFree.Call(hMem)
		return errors.New("GlobalLock failed")
	}
	// LazyProc.Call is not recognized as a syscall boundary by vet, so the
	// pointer rides through a dereference (unsafe rule 3 workaround).
	buf := unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&ptr))), len(payload))
	copy(buf, payload)
	pGUnlock.Call(hMem)

	if ret, _, _ := pOpenClp.Call(0); ret == 0 {
		pGFree.Call(hMem)
		return errors.New("could not open the clipboard")
	}
	defer pCloseCl.Call()
	pEmptyCl.Call()
	if ret, _, _ := pSetData.Call(cfHDROP, hMem); ret == 0 {
		pGFree.Call(hMem)
		return errors.New("setting the clipboard failed")
	}
	return nil
}
