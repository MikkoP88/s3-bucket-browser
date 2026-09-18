package api

import "fmt"

// EventExitConfirm asks the frontend to confirm an exit that would lose
// work (running transfers, running tasks, unsaved profile changes). Payload: {reason}.
const EventExitConfirm = "exit:confirm"

// exitBusyReason returns why the app should not exit right now, or "" for
// a clean exit: any running transfer job, any running registry task, a
// dirty open profile file, or unsaved session sources (the same states
// the profile bar badges).
func (a *App) exitBusyReason() string {
	running := 0
	var first JobInfo
	for _, j := range a.jobs.snapshot() {
		if j.Status == JobRunning {
			running++
			if running == 1 {
				first = j
			}
		}
	}
	if running > 0 {
		return fmt.Sprintf("%d transfer job(s) still running (e.g. %s: %d/%d file(s))",
			running, first.Op, first.DoneFiles, first.TotalFiles)
	}

	// Registry tasks are the everything-else work: deep searches, bulk
	// deletes, version purges, bucket emptying, class conversions, copies.
	// 'list' rows are transient navigation (dropped on finish) and block
	// nothing — quitting mid-listing loses no work.
	running = 0
	var firstTask TaskInfo
	for _, tk := range a.tasks.snapshot() {
		if tk.Kind == "list" || (tk.Status != TaskRunning && tk.Status != TaskQueued) {
			continue
		}
		running++
		if running == 1 {
			firstTask = tk
		}
	}
	if running > 0 {
		return fmt.Sprintf("%d task(s) still running (e.g. %s: %s)", running, firstTask.Kind, firstTask.Label)
	}

	st := a.GetProfileFileState()
	if st.Open && st.Dirty {
		return fmt.Sprintf("the profile file %q has unsaved changes", st.Name)
	}
	if !st.Open && st.SourceCount > 0 {
		return fmt.Sprintf("%d session source(s) are unsaved", st.SourceCount)
	}
	return ""
}

// ExitApp quits the application (menu bar "Exit"). Guarded: while work is
// in progress the first call only asks — the exit:confirm event carries
// the reason and the frontend shows the confirmation dialog; ConfirmExit
// force-quits past it.
func (a *App) ExitApp() {
	if reason := a.exitBusyReason(); reason != "" && !a.exitOK.Load() {
		a.emit(EventExitConfirm, map[string]string{"reason": reason})
		return
	}
	a.exitOK.Store(true)
	if a.ctx != nil && shell != nil {
		shell.Quit()
	}
}

// ConfirmExit quits despite the busy reason — the confirmation dialog's
// "Exit anyway".
func (a *App) ConfirmExit() {
	a.exitOK.Store(true)
	if a.ctx != nil && shell != nil {
		shell.Quit()
	}
}

// ShouldClose gates the main window's close (the X button): a busy app
// vetoes the close and asks through the frontend instead (exit:confirm);
// a confirmed or clean close proceeds. Returns true = prevent the close.
func (a *App) ShouldClose() bool {
	if a.exitOK.Load() {
		return false // confirmed exit in flight — let the window close
	}
	reason := a.exitBusyReason()
	if reason == "" {
		return false
	}
	a.emit(EventExitConfirm, map[string]string{"reason": reason})
	return true // prevent: ask first
}
