//go:build windows

package api

import (
	"errors"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32          = windows.NewLazySystemDLL("ole32.dll")
	pCoInitEx      = ole32.NewProc("CoInitializeEx")
	pCoUninit      = ole32.NewProc("CoUninitialize")
	pCoCreateInst  = ole32.NewProc("CoCreateInstance")
	pCoTaskMemFree = ole32.NewProc("CoTaskMemFree")
)

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1
	sOK                     = 0
	rpcEChangedMode         = 0x80010106
	// IFileDialog options: multi-select of existing files AND folders.
	fosAllowMultiSelect = 0x200
	fosPathMustExist    = 0x800
	fosFileMustExist    = 0x1000
	fosNoChangeDir      = 0x8
)

// vcall invokes a COM interface method by vtable index.
func vcall(obj unsafe.Pointer, method int, args ...uintptr) uintptr {
	vt := *(*unsafe.Pointer)(obj)
	sz := unsafe.Sizeof(uintptr(0))
	fn := *(*uintptr)(unsafe.Add(vt, sz*uintptr(method)))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

// pickUploadItems: raw IFileOpenDialog. FOS_ALLOWMULTISELECT without
// FOS_PICKFOLDERS is the one mode where a single dialog returns both files
// and folders (Explorer's own attach dialog behavior) — the Wails wrappers
// force either/or.
func pickUploadItems(a *App) ([]string, error) {
	if a.ctx == nil {
		return nil, errNoContext
	}
	needUninit := false
	if hr, _, _ := pCoInitEx.Call(0, coinitApartmentThreaded); hr == sOK {
		needUninit = true
	} else if hr != rpcEChangedMode && hr != 1 { // S_FALSE(1): already inited on this thread
		return nil, errors.New("COM init failed")
	}
	if needUninit {
		defer pCoUninit.Call()
	}

	clsid, err := windows.GUIDFromString("{DC1C5A9C-E88A-4dde-A5A1-60F82A20AEF7}") // CLSID_FileOpenDialog
	if err != nil {
		return nil, err
	}
	iid, err := windows.GUIDFromString("{D57C7288-D4AD-4768-BE02-9D969532D960}") // IID_IFileOpenDialog
	if err != nil {
		return nil, err
	}
	var dlg unsafe.Pointer
	if hr, _, _ := pCoCreateInst.Call(
		uintptr(unsafe.Pointer(&clsid)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&dlg))); hr != sOK || dlg == nil {
		return nil, errors.New("could not create the file dialog")
	}
	defer vcall(dlg, 2) // Release

	vcall(dlg, 9, fosAllowMultiSelect|fosPathMustExist|fosFileMustExist|fosNoChangeDir)                        // SetOptions
	vcall(dlg, 14, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Select files and/or folders to upload")))) // SetTitle
	if hr := vcall(dlg, 3, 0); hr != sOK {                                                                     // Show — non-zero is cancel/failure
		if hr == uintptr(0x800704C7) { // ERROR_CANCELLED
			return nil, nil
		}
		return nil, os.NewSyscallError("dialog show", syscall.Errno(hr&0xFFFF))
	}

	var results unsafe.Pointer
	if hr := vcall(dlg, 22, uintptr(unsafe.Pointer(&results))); hr != sOK || results == nil { // GetResults
		return nil, errors.New("dialog returned no selection")
	}
	defer vcall(results, 2) // Release

	var n uintptr
	vcall(results, 7, uintptr(unsafe.Pointer(&n))) // GetCount
	out := make([]string, 0, n)
	for i := uintptr(0); i < n; i++ {
		var item unsafe.Pointer
		if vcall(results, 8, i, uintptr(unsafe.Pointer(&item))) != sOK { // GetItemAt
			continue
		}
		var name *uint16
		if vcall(item, 5, 0x80058000, uintptr(unsafe.Pointer(&name))) == sOK && name != nil { // GetDisplayName(SIGDN_FILESYSPATH)
			out = append(out, windows.UTF16PtrToString(name))
			pCoTaskMemFree.Call(uintptr(unsafe.Pointer(name)))
		}
		vcall(item, 2) // Release
	}
	return out, nil
}

func stageClipboardDir() (string, error) {
	return os.MkdirTemp("", "s3b-clip-")
}
