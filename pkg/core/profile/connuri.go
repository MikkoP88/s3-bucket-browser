// connuri.go holds the connection-URL grammar shared by the CLI's
// `source add [NAME] URL` shorthand and the GUI path bar's
// external-address form: scheme://user:pass@host:port/root.
package profile

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ConnURI is a parsed sftp:// scp:// ftp:// ftps:// webdav://
// webdavs:// connection URL.
type ConnURI struct {
	Type     string
	Host     string
	Port     int
	Username string
	Password string
	Root     string
}

// ParseS3BucketURI matches the s3://BUCKET shorthand (ok=false for
// anything else; ok=true with an empty bucket means malformed).
func ParseS3BucketURI(raw string) (bucket string, ok bool) {
	if !strings.HasPrefix(strings.ToLower(raw), "s3://") {
		return "", false
	}
	b := strings.Trim(raw[5:], "/")
	if b == "" || strings.ContainsAny(b, "@:/?#") {
		return "", true
	}
	return b, true
}

// ParseConnURI parses a scheme://user:pass@host:port/root connection
// URL. ok=false for anything that is not one of the six remote schemes
// (plain names, s3:// URIs); err carries malformed-URL detail when ok=true.
func ParseConnURI(raw string) (u ConnURI, ok bool, err error) {
	i := strings.Index(raw, "://")
	if i <= 0 {
		return ConnURI{}, false, nil
	}
	typ, known := map[string]string{
		"sftp":    TypeSFTP,
		"scp":     TypeSCP,
		"ftp":     TypeFTP,
		"ftps":    TypeFTPS,
		"webdav":  TypeWebDAV,
		"webdavs": TypeWebDAVS,
	}[strings.ToLower(raw[:i])]
	if !known {
		return ConnURI{}, false, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ConnURI{}, true, fmt.Errorf("malformed URL %q: %v", raw, err)
	}
	if parsed.Hostname() == "" {
		return ConnURI{}, true, fmt.Errorf("URL %q has no host", raw)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ConnURI{}, true, fmt.Errorf("URL %q: query strings and fragments are not valid here", raw)
	}
	u = ConnURI{Type: typ, Host: parsed.Hostname(), Username: parsed.User.Username()}
	u.Password, _ = parsed.User.Password()
	if p := parsed.Port(); p != "" {
		u.Port, err = strconv.Atoi(p)
		if err != nil || u.Port < 1 || u.Port > 65535 {
			return ConnURI{}, true, fmt.Errorf("URL %q: invalid port %q", raw, p)
		}
	}
	// Root keeps the URL's leading slash: both engines anchor "/" at
	// Root ("/srv/data"), and an empty path stays the login directory.
	u.Root = strings.TrimSuffix(parsed.Path, "/")
	return u, true, nil
}
