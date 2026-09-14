//go:build !windows

package api

import "errors"

func osClipboardFiles() []string { return nil }

func osClipboardSetFiles([]string) error {
	return errors.New("OS file clipboard is only supported on Windows")
}
