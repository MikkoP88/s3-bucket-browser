//go:build windows

package guihealth

import (
	"golang.org/x/sys/windows/registry"
)

// The Evergreen runtime's EdgeUpdate registration, the same place the
// WebView2 loader itself looks: a Clients subkey named by the runtime
// GUID, its "pv" value carrying the version. Three locations cover every
// install shape — machine-wide (the 32-bit EdgeUpdate view under
// WOW6432Node, which is where a 64-bit process finds it), the native
// 64-bit view, and a per-user install under HKCU.
const webview2RuntimeGUID = `{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`

var webview2RegistryLocations = []struct {
	root registry.Key
	path string
}{
	{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\` + webview2RuntimeGUID},
	{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\EdgeUpdate\Clients\` + webview2RuntimeGUID},
	{registry.CURRENT_USER, `Software\Microsoft\EdgeUpdate\Clients\` + webview2RuntimeGUID},
}

func init() { probeWebView2Runtime = probeWebView2Registry }

// probeWebView2Registry answers from the registry. A location that
// reports a usable pv settles the question at once; a location that is
// simply absent (ERROR_FILE_NOT_FOUND / ERROR_PATH_NOT_FOUND) says "not
// installed from that hive" and the next one is tried. The answer only
// becomes the definitive "missing" (known=true, pv="") once at least one
// location was readable — an unreadable registry must never block a
// launch.
func probeWebView2Registry() (string, bool) {
	askable := false
	for _, loc := range webview2RegistryLocations {
		key, err := registry.OpenKey(loc.root, loc.path, registry.QUERY_VALUE)
		if err != nil {
			continue // absent from this hive, or unreadable — either way, next
		}
		askable = true
		pv, _, err := key.GetStringValue("pv")
		key.Close()
		if err == nil && webview2VersionUsable(pv) {
			return pv, true
		}
	}
	if !askable {
		return "", false // could not ask the registry at all — fail open
	}
	return "", true // asked everywhere: no usable runtime on this machine
}
