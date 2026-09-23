// openurl.go: the OpenExternal binding — opens http(s) URLs in the
// user's default browser. Used by the license/repo links in the Help
// views. The scheme is allow-listed because a webview-rendered link must
// never reach file://, custom OS scheme handlers or the shell: only
// http and https, and only with a host, get through.
package api

import (
	"fmt"
	"net/url"
)

// OpenExternal opens raw in the user's default browser.
func (a *App) OpenExternal(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("refusing to open %q as an external URL", raw)
	}
	return openExternal(u.String())
}
