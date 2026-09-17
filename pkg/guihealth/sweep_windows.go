//go:build windows

package guihealth

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func init() {
	listProcs = snapshotProcs
	terminateProc = terminatePID
}

// snapshotProcs walks the Toolhelp32 process snapshot: pid, parent pid and
// image name for every process on the system.
func snapshotProcs() ([]procInfo, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	out := []procInfo{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	if err := windows.Process32First(snap, &e); err != nil {
		return out, nil // an empty snapshot is empty, not a failure worth blocking on
	}
	for {
		out = append(out, procInfo{
			pid:  e.ProcessID,
			ppid: e.ParentProcessID,
			name: windows.UTF16ToString(e.ExeFile[:]),
		})
		if err := windows.Process32Next(snap, &e); err != nil {
			break
		}
	}
	return out, nil
}

// terminatePID force-terminates one process. An error here usually means
// it is already gone, which the sweep counts as success.
func terminatePID(pid uint32) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}
