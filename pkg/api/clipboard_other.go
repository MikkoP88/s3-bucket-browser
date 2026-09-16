//go:build !windows

package api

import "errors"

func osClipboardFiles() []string { return nil }

func osClipboardSeq() uint64 { return 0 }

func osClipboardHasFiles() bool { return false }

func osClipboardSetFiles([]string) error {
	return errors.New("OS file clipboard is only supported on Windows")
}
