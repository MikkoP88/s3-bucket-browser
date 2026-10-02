//go:build !windows || s3b_headless || server

package gui

// guardSingleInstance is Run's front door on builds without the Windows
// desktop guard: never another instance, never a focus signal. Server
// builds (-tags server, the gui-live rig and the verify batteries) and
// the headless CLI are multi-instance by design, and macOS/Linux desktops
// get single-launch semantics from their own windowing systems
// (LaunchServices, desktop-environment single-window handling) — the
// double-launch hazard the mutex guards against is a Windows shape.
func guardSingleInstance() (focus <-chan struct{}, existing bool) {
	return nil, false
}
