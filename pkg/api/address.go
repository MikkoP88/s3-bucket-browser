// address.go resolves one pasted address — the single ladder both path
// editors (the main pane's and the secondary pane's) submit their lines
// to. App paths name a configured data source directly: NAME://contents,
// TYPE://Name/contents (the scheme is the source's type) or the editors'
// normalized display form Name/contents; s3:// and
// connection URIs (sftp://user:pass@host:port/root)
// resolve against the workspace's configured sources, standing one up on
// the fly when nothing matches (NewSource — the caller saves it through
// SaveSource, then navigates); file:/// and bare local paths address the
// workstation's filesystem.
package api

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// AddressResult is one resolved address: enough to navigate (Kind +
// Source/Bucket/Prefix, in the editors' location vocabulary) plus, when
// the address carried a connection nothing configured matches, the
// source to save first.
type AddressResult struct {
	Kind      string          `json:"kind"`                // objects | buckets | remote | local
	Source    string          `json:"source"`              // data source name to navigate
	Bucket    string          `json:"bucket,omitempty"`    // objects
	Prefix    string          `json:"prefix,omitempty"`    // s3 prefix | remote path | local dir
	NewSource *profile.Source `json:"newSource,omitempty"` // unconfigured: SaveSource, then navigate
}

// ParseAddress maps any pasted address to a location. hint is the
// editor's current source name ("" when none) — it breaks s3:// ties
// when several S3 accounts could own the bucket.
func (a *App) ParseAddress(raw, hint string) (*AddressResult, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty address")
	}
	srcs := a.workspaceSources()
	if i := strings.Index(raw, "://"); i > 0 {
		scheme, rest := raw[:i], raw[i+3:]
		// 1. NAME:// — the scheme names a configured source (by name,
		// case-insensitively, then by id).
		for idx := range srcs {
			s := &srcs[idx]
			if strings.EqualFold(s.Name, scheme) || (s.ID != "" && strings.EqualFold(s.ID, scheme)) {
				return sourceLoc(s, rest), nil
			}
		}
		// 1b. TYPE://Name/contents — the scheme is the data source's
		// TYPE (s3, sftp, ftp, ...) and the first segment after it
		// names a configured source OF THAT TYPE: the canonical address
		// form every app-added data source carries. Real connection
		// URIs never match — their authority segment carries one of
		// @, :, ?, # — and fall through to step 4, as does a first
		// segment naming no source of the type.
		if typ, typed := profile.SchemeType(scheme); typed {
			seg, content := rest, ""
			if j := strings.IndexAny(rest, `/\`); j >= 0 {
				seg, content = rest[:j], rest[j+1:]
			}
			if seg != "" && !strings.ContainsAny(seg, "@:?#") {
				for idx := range srcs {
					s := &srcs[idx]
					if s.Type == typ &&
						(strings.EqualFold(s.Name, seg) || (s.ID != "" && strings.EqualFold(s.ID, seg))) {
						return sourceLoc(s, content), nil
					}
				}
			}
		}
		// 2. s3://bucket/key — resolve which account owns it (a first
		// segment naming an S3 source already won at 1b).
		if strings.EqualFold(scheme, "s3") {
			return s3Loc(srcs, rest, hint)
		}
		// 3. local:// — the local surface's own canonical form (the
		// secondary pane's path editor): the workstation directory, or
		// the filesystem roots when empty. A configured source named
		// "local" already won above.
		if strings.EqualFold(scheme, "local") {
			return &AddressResult{Kind: "local", Prefix: filepath.FromSlash(strings.ReplaceAll(rest, "\\", "/"))}, nil
		}
		// 4. connection URIs: match a configured source, else stand one up.
		if u, ok, err := profile.ParseConnURI(raw); ok {
			if err != nil {
				return nil, err
			}
			return connLoc(srcs, u)
		}
		// 5. file:/// — the workstation's filesystem (a host is a UNC share).
		if strings.EqualFold(scheme, "file") {
			return fileLoc(raw)
		}
		return nil, fmt.Errorf("unrecognized address %q", raw)
	}
	// 6. bare local paths: drive letters, UNC shares, ~.
	if dir, ok := bareLocalDir(raw); ok {
		return &AddressResult{Kind: "local", Prefix: dir}, nil
	}
	// 7. the normalized display form — Name/contents, no scheme: the
	// first segment (or the whole line) names a configured source (by
	// name or id, case-insensitively) and the rest is its contents —
	// exactly what the path editors show, so a copied path pastes
	// straight back. Local addresses already won at step 6 (drive
	// letters, UNC, ~); a relative path naming no source stays
	// unrecognized.
	seg, rest := raw, ""
	if i := strings.IndexAny(raw, `/\`); i > 0 {
		seg, rest = raw[:i], raw[i+1:]
	}
	for idx := range srcs {
		s := &srcs[idx]
		if strings.EqualFold(s.Name, seg) || (s.ID != "" && strings.EqualFold(s.ID, seg)) {
			return sourceLoc(s, rest), nil
		}
	}
	return nil, fmt.Errorf("unrecognized address %q", raw)
}

// sourceLoc maps one configured source's contents (NAME://rest or the
// normalized Name/rest) — the grammar the path editors have always
// spoken: backslashes fold to slashes, a
// bucket-scoped S3 source takes the whole rest as content inside its one
// bucket (the legacy doubled form folds away), an account-wide S3 source
// splits bucket then prefix, and every other type (remote engines and
// local roots alike) navigates a /-anchored path.
func sourceLoc(s *profile.Source, rest string) *AddressResult {
	rest = strings.ReplaceAll(rest, `\`, "/")
	rest = strings.TrimLeft(rest, "/")
	if s.Type == profile.TypeS3 {
		if s.Bucket != "" {
			content := rest
			if content == s.Bucket {
				content = ""
			} else if strings.HasPrefix(content, s.Bucket+"/") {
				content = content[len(s.Bucket)+1:]
			}
			prefix := ""
			if content != "" {
				prefix = strings.TrimRight(content, "/") + "/"
			}
			return &AddressResult{Kind: "objects", Source: s.Name, Bucket: s.Bucket, Prefix: prefix}
		}
		if rest == "" {
			return &AddressResult{Kind: "buckets", Source: s.Name}
		}
		bucket, key, _ := strings.Cut(rest, "/")
		prefix := ""
		if key != "" {
			prefix = strings.TrimRight(key, "/") + "/"
		}
		return &AddressResult{Kind: "objects", Source: s.Name, Bucket: bucket, Prefix: prefix}
	}
	path := "/"
	if rest != "" {
		path = "/" + strings.TrimRight(rest, "/") + "/"
	}
	return &AddressResult{Kind: "remote", Source: s.Name, Prefix: path}
}

// s3Loc resolves s3://bucket[/key]: a source scoped to that bucket wins,
// else the editor's own source when it is an account-wide S3 one, else
// the workspace's only account-wide S3 source — several candidates is an
// error naming them (the NAME:// form says which account outright).
func s3Loc(srcs []profile.Source, rest, hint string) (*AddressResult, error) {
	bucket, key, _ := strings.Cut(rest, "/")
	bucket = strings.Trim(bucket, "/")
	if bucket == "" || strings.ContainsAny(bucket, "@:?#") {
		return nil, errors.New("s3:// address must name a bucket: s3://my-bucket/path")
	}
	var s3s []int
	for idx := range srcs {
		if srcs[idx].Type == profile.TypeS3 {
			s3s = append(s3s, idx)
		}
	}
	pick := -1
	for _, idx := range s3s {
		if srcs[idx].Bucket == bucket {
			pick = idx // scoped to exactly this bucket — the strongest match
			break
		}
	}
	if pick < 0 && hint != "" {
		for _, idx := range s3s {
			if srcs[idx].Bucket == "" &&
				(strings.EqualFold(srcs[idx].Name, hint) || (srcs[idx].ID != "" && strings.EqualFold(srcs[idx].ID, hint))) {
				pick = idx
				break
			}
		}
	}
	if pick < 0 {
		var accountWide []int
		for _, idx := range s3s {
			if srcs[idx].Bucket == "" {
				accountWide = append(accountWide, idx)
			}
		}
		if len(accountWide) == 1 {
			pick = accountWide[0]
		}
	}
	if pick < 0 {
		names := make([]string, 0, len(s3s))
		for _, idx := range s3s {
			names = append(names, srcs[idx].Name)
		}
		if len(names) == 0 {
			return nil, errors.New("no S3 data source is configured — add one first")
		}
		return nil, fmt.Errorf("several S3 data sources could own %q — name one (NAME://…): %s",
			bucket, strings.Join(names, ", "))
	}
	s := &srcs[pick]
	prefix := ""
	if key != "" {
		prefix = strings.TrimRight(key, "/") + "/"
	}
	if s.Bucket != "" {
		// scoped source: the key is content inside its one bucket
		return &AddressResult{Kind: "objects", Source: s.Name, Bucket: s.Bucket, Prefix: prefix}, nil
	}
	return &AddressResult{Kind: "objects", Source: s.Name, Bucket: bucket, Prefix: prefix}, nil
}

// connLoc resolves a connection URI: a configured source of the same
// type, host and user opens at the URI's root; nothing matches means the
// address itself carries the connection — stand the source up (the
// caller saves it, then navigates).
func connLoc(srcs []profile.Source, u profile.ConnURI) (*AddressResult, error) {
	path := "/"
	if u.Root != "" {
		path = u.Root + "/"
	}
	for idx := range srcs {
		s := &srcs[idx]
		if s.Type == u.Type && strings.EqualFold(s.Host, u.Host) && s.Username == u.Username {
			return &AddressResult{Kind: "remote", Source: s.Name, Prefix: path}, nil
		}
	}
	// the auto-name follows the source editor's convention: the host,
	// or "<host> - <start dir>" once a root narrows it
	name := u.Host
	if u.Root != "" {
		name = fmt.Sprintf("%s - %s", u.Host, strings.TrimPrefix(u.Root, "/"))
	}
	ns := &profile.Source{
		Name:     name,
		Type:     u.Type,
		Host:     u.Host,
		Port:     u.Port,
		Username: u.Username,
		Password: u.Password,
		Root:     u.Root,
	}
	return &AddressResult{Kind: "remote", Source: name, Prefix: path, NewSource: ns}, nil
}

// fileLoc resolves file:/// URLs to workstation directories: an
// authority is a UNC share (file://server/share/x → \\server\share\x),
// a bare path with a drive letter folds its leading slash away
// (file:///C:/Users → C:\Users), any other absolute form passes through
// as written.
func fileLoc(raw string) (*AddressResult, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("malformed file URL: %v", err)
	}
	if u.Host != "" {
		return &AddressResult{Kind: "local", Prefix: `\\` + u.Host + filepath.FromSlash(u.Path)}, nil
	}
	p := u.Path
	if len(p) > 2 && p[0] == '/' && isDriveLetter(p[1]) && p[2] == ':' {
		p = filepath.FromSlash(p[1:]) // /C:/x → C:\x
	}
	return &AddressResult{Kind: "local", Prefix: p}, nil
}

// bareLocalDir recognizes bare workstation paths: drive letters
// (C:\Projects), UNC shares (\\server\share) and ~ (the home directory,
// with ~\rest expanding under it). Anything else — including
// /-anchored unix paths, ambiguous with remote roots — is not a local
// address.
func bareLocalDir(raw string) (string, bool) {
	if raw == "~" || strings.HasPrefix(raw, `~\`) || strings.HasPrefix(raw, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		return filepath.Join(home, strings.TrimPrefix(raw, "~")), true
	}
	if len(raw) >= 3 && isDriveLetter(raw[0]) && raw[1] == ':' && (raw[2] == '\\' || raw[2] == '/') {
		return filepath.FromSlash(raw), true
	}
	if strings.HasPrefix(raw, `\\`) || strings.HasPrefix(raw, "//") {
		return filepath.FromSlash(raw), true
	}
	return "", false
}

func isDriveLetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
