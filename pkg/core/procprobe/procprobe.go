// Package procprobe answers one question: is a process with this PID
// running right now? The multi-instance workspaces (pkg/api) and the
// per-instance WebView2 profiles (gui) both sweep crash leftovers at
// boot, and a sweep must never delete the workspace of an instance that
// is still alive — so liveness is the load-bearing fact.
//
// The answer is fail-safe: a PID we cannot ask about (access denied to a
// process owned by another user, a protected system process) counts as
// alive, because the cost of a wrong "alive" is one leftover directory
// while the cost of a wrong "dead" is deleting a running app's files.
package procprobe

// Alive reports whether a process with the given PID exists right now.
// PIDs <= 0 are never alive. The check is advisory, not a lock: a PID can
// die right after the probe answers (the sweep treats that as one more
// boot's work) or be recycled by the OS onto an unrelated live process
// (which then keeps a stale directory alive until that process exits —
// disk residue, never corruption).
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return alive(pid)
}
