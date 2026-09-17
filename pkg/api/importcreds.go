package api

// Import credentials — one flow for every origin:
//   - local credential files (AWS INI, rclone-style INI, JSON exports, .env,
//     encrypted .s3bprofile files) — any data source type, not just S3
//   - secrets services (KMS): HashiCorp Vault, AWS Secrets Manager,
//     Azure Key Vault, GCP Secret Manager, custom HTTP JSON endpoints
//
// Secrets never travel to the frontend: parsed candidates are stashed
// Go-side under a random id; the dialog only sees metadata. Test and
// import work from the stash, so the flow is end-to-end: fetch → pick →
// verify (bucket count) → add to the workspace.

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/aws/aws-sdk-go-v2/aws"
)

// CredCandidate is the metadata of one importable connection (secrets stay
// in the Go-side stash).
type CredCandidate struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Endpoint  string `json:"endpoint,omitempty"`
	Region    string `json:"region,omitempty"`
	Host      string `json:"host,omitempty"`
	Username  string `json:"username,omitempty"`
	HasSecret bool   `json:"hasSecret"`
	Origin    string `json:"origin,omitempty"`
}

type pendingCred struct {
	src     profile.Source
	expires time.Time
}

// PickCredentialFiles opens the native multi-select file dialog for
// credential/config files.
func (a *App) PickCredentialFiles() ([]string, error) {
	if shell == nil {
		return nil, errNoShell
	}
	return shell.OpenFiles(ShellDialog{
		Title: "Select credential files",
		Filters: []ShellFilter{
			{DisplayName: "Credential files (*.ini;*.cfg;*.conf;*.env;*.json;*.s3bprofile)",
				Pattern: "*.ini;*.cfg;*.conf;*.env;*.json;*.s3bprofile"},
			{DisplayName: "All files (*.*)", Pattern: "*.*"},
		},
	})
}

// ParseCredentialFile reads one local credential file and returns its
// importable connections. password is used only for .s3bprofile containers.
func (a *App) ParseCredentialFile(path, password string) ([]CredCandidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(data)
	switch {
	case strings.HasPrefix(strings.TrimSpace(text), "s3bpf|"):
		return a.candidatesFromContainer(data, password, path)
	case json.Valid(data):
		return a.candidatesFromJSON(data, path)
	case strings.Contains(text, "="):
		return a.candidatesFromINI(text, path)
	}
	return nil, fmt.Errorf("%s: unrecognized credential file format", filepath.Base(path))
}

// KmsFetch pulls secrets from a KMS/secrets service and turns every JSON
// payload with recognizable credentials into candidates.
func (a *App) KmsFetch(service string, params map[string]string) ([]CredCandidate, error) {
	var payloads []map[string]any
	switch service {
	case "vault":
		p, err := kmsVault(params)
		if err != nil {
			return nil, err
		}
		payloads = p
	case "awssm":
		p, err := kmsAwsSecretsManager(params)
		if err != nil {
			return nil, err
		}
		payloads = p
	case "azure":
		p, err := kmsAzureKeyVault(params)
		if err != nil {
			return nil, err
		}
		payloads = p
	case "gcp":
		p, err := kmsGcpSecretManager(params)
		if err != nil {
			return nil, err
		}
		payloads = p
	case "http":
		p, err := kmsCustomHTTP(params)
		if err != nil {
			return nil, err
		}
		payloads = p
	default:
		return nil, fmt.Errorf("unknown secrets service %q", service)
	}
	a.emitLog(LogInfo, "import", fmt.Sprintf("fetched %d candidate(s) from %s", len(payloads), serviceLabel(service, params)))
	return a.candidatesFromPayloads(payloads, serviceLabel(service, params)), nil
}

func serviceLabel(service string, params map[string]string) string {
	switch service {
	case "vault":
		return "Vault"
	case "awssm":
		return "AWS Secrets Manager"
	case "azure":
		return "Azure Key Vault"
	case "gcp":
		return "GCP Secret Manager"
	default:
		if u := params["url"]; u != "" {
			return u
		}
		return "HTTP endpoint"
	}
}

// TestCredentialDraft verifies one stashed candidate without importing it
// (S3: bucket count; remote: root listing).
func (a *App) TestCredentialDraft(id string) TestResult {
	src, ok := a.takePending(id, false)
	if !ok {
		return TestResult{OK: false, Message: "candidate expired — fetch again"}
	}
	if src.Type == profile.TypeS3 && src.S3 != nil {
		if a.ctx == nil {
			return TestResult{OK: false, Message: errNoContext.Error()}
		}
		c, err := s3client.New(a.ctx, *src.S3, s3client.Options{Timeout: 0})
		if err != nil {
			return TestResult{OK: false, Message: err.Error()}
		}
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		defer cancel()
		if src.Bucket != "" {
			return probeBucket(ctx, c, src.Bucket)
		}
		return probeBuckets(ctx, c)
	}
	return a.testRemoteSource(src)
}

// ImportCredentials adds the selected candidates to the workspace. S3
// candidates are bucket-scoped: every bucket the credential can see
// becomes its own data source, named after the bucket. Re-imports are
// idempotent — a candidate matching an existing source by connection
// (endpoint + access key + bucket) refreshes it in place, and only
// buckets without a source are added; nothing is ever duplicated.
func (a *App) ImportCredentials(ids []string) (ImportResult, error) {
	res := ImportResult{Imported: []string{}, Updated: []string{}, Skipped: []string{}}
	for _, id := range ids {
		src, ok := a.takePending(id, true)
		if !ok {
			res.Skipped = append(res.Skipped, id)
			continue
		}
		if src.Type == profile.TypeS3 && src.S3 != nil && src.Bucket == "" {
			names, err := a.listCandidateBuckets(src)
			if err == nil && len(names) > 0 {
				for _, bucket := range names {
					bktSrc := src
					p := *src.S3
					p.Name = bucket
					bktSrc.S3 = &p
					bktSrc.Name = bucket
					bktSrc.Bucket = bucket
					a.importSource(bktSrc, &res)
				}
				continue
			}
			// Bucket listing failed or sees nothing (a scoped-down
			// credential): fall through and keep the account-wide source
			// so the candidate is not lost.
		}
		a.importSource(src, &res)
	}
	a.emitLog(LogInfo, "import", fmt.Sprintf("import finished: %d added, %d updated, %d skipped",
		len(res.Imported), len(res.Updated), len(res.Skipped)))
	return res, nil
}

// importSource upserts one candidate: a workspace source matching it by
// ID or connection is refreshed in place (name, color and timestamps
// kept); otherwise a new uniquely-named source is created.
func (a *App) importSource(src profile.Source, res *ImportResult) {
	ex := a.matchingSource(src)
	if ex != nil {
		src.ID, src.Name, src.Color = ex.ID, ex.Name, ex.Color
	} else {
		src.Name = a.uniqueSourceName(src.Name)
	}
	if err := a.SaveSource(src); err != nil {
		res.Skipped = append(res.Skipped, src.Name)
		return
	}
	if ex != nil {
		res.Updated = append(res.Updated, src.Name)
	} else {
		res.Imported = append(res.Imported, src.Name)
	}
}

// matchingSource finds the workspace source a candidate would update:
// the same ID (a container re-import), or the same connection. nil when
// the candidate is new.
func (a *App) matchingSource(src profile.Source) *profile.Source {
	srcs := a.workspaceSources()
	if src.ID != "" {
		for i := range srcs {
			if srcs[i].ID == src.ID {
				return &srcs[i]
			}
		}
	}
	for i := range srcs {
		if sameConnection(srcs[i], src) {
			return &srcs[i]
		}
	}
	return nil
}

// sameConnection reports whether two sources describe the same
// connection: for S3, endpoint + access key + the scoped bucket; for
// remote engines, host + port + username.
func sameConnection(a, b profile.Source) bool {
	if a.Type != b.Type {
		return false
	}
	if a.Type == profile.TypeS3 {
		return a.Bucket == b.Bucket && a.S3 != nil && b.S3 != nil &&
			a.S3.AccessKeyID == b.S3.AccessKeyID &&
			strings.EqualFold(strings.TrimSuffix(a.S3.Endpoint, "/"), strings.TrimSuffix(b.S3.Endpoint, "/"))
	}
	return a.Host == b.Host && a.Port == b.Port && a.Username == b.Username
}

// listCandidateBuckets dials a candidate's credentials and returns the
// bucket names visible to them (the per-bucket expansion of an import).
func (a *App) listCandidateBuckets(src profile.Source) ([]string, error) {
	if a.ctx == nil {
		return nil, errNoContext
	}
	c, err := s3client.New(a.ctx, *src.S3, s3client.Options{Timeout: 0})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	buckets, err := listing.ListBuckets(ctx, c.S3)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(buckets))
	for _, b := range buckets {
		if n := aws.ToString(b.Name); n != "" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names, nil
}

// uniqueSourceName appends -2, -3, … while the name is taken by another
// source (imports must never silently overwrite an existing connection).
func (a *App) uniqueSourceName(name string) string {
	existing := map[string]bool{}
	for _, s := range a.workspaceSources() {
		existing[strings.ToLower(s.Name)] = true
	}
	if !existing[strings.ToLower(name)] {
		return name
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d", name, i)
		if !existing[strings.ToLower(cand)] {
			return cand
		}
	}
}

// takePending reads (and optionally removes) one stash entry.
func (a *App) takePending(id string, remove bool) (profile.Source, bool) {
	a.pendingMu.Lock()
	defer a.pendingMu.Unlock()
	if a.pendingCreds == nil {
		return profile.Source{}, false
	}
	pc, ok := a.pendingCreds[id]
	if !ok || time.Now().After(pc.expires) {
		if ok {
			delete(a.pendingCreds, id)
		}
		return profile.Source{}, false
	}
	if remove {
		delete(a.pendingCreds, id)
	}
	return pc.src, true
}

func (a *App) stashCred(src profile.Source) string {
	id := randToken()
	a.pendingMu.Lock()
	if a.pendingCreds == nil {
		a.pendingCreds = map[string]pendingCred{}
	}
	// opportunistic cleanup
	for k, pc := range a.pendingCreds {
		if time.Now().After(pc.expires) {
			delete(a.pendingCreds, k)
		}
	}
	a.pendingCreds[id] = pendingCred{src: src, expires: time.Now().Add(30 * time.Minute)}
	a.pendingMu.Unlock()
	return id
}

// ---------------------------- file parsers ----------------------------

func (a *App) candidatesFromContainer(data []byte, password, origin string) ([]CredCandidate, error) {
	if password == "" {
		return nil, errors.New("this is an encrypted profile file — enter its password")
	}
	_, srcs, err := profile.DecryptContainer(data, password)
	if err != nil {
		return nil, err
	}
	out := []CredCandidate{}
	for _, s := range srcs {
		p := s
		out = append(out, CredCandidate{
			ID:        a.stashCred(p),
			Name:      p.Name,
			Type:      p.Type,
			Endpoint:  endpointOf(p),
			Region:    regionOf(p),
			Host:      p.Host,
			Username:  p.Username,
			HasSecret: hasSecret(p),
			Origin:    filepath.Base(origin),
		})
	}
	return out, nil
}

func (a *App) candidatesFromJSON(data []byte, origin string) ([]CredCandidate, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	payloads := flattenJSON(v)
	return a.candidatesFromPayloads(payloads, filepath.Base(origin)), nil
}

// flattenJSON surfaces every object in the document that carries
// credential-ish keys (the top object, nested maps, and array items).
func flattenJSON(v any) []map[string]any {
	var out []map[string]any
	var walk func(x any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			if looksLikeCreds(t) {
				out = append(out, t)
			}
			for _, vv := range t {
				walk(vv)
			}
		case []any:
			for _, vv := range t {
				walk(vv)
			}
		}
	}
	walk(v)
	return out
}

func looksLikeCreds(m map[string]any) bool {
	for _, k := range credAliases["access"] {
		if _, ok := m[k]; ok {
			return true
		}
	}
	for _, k := range []string{"host", "server"} {
		if _, ok := m[k]; ok {
			if _, ok2 := m["user"]; ok2 {
				return true
			}
			if _, ok2 := m["username"]; ok2 {
				return true
			}
		}
	}
	return false
}

func (a *App) candidatesFromINI(text, origin string) ([]CredCandidate, error) {
	sections := parseGenericINI(text)
	out := []CredCandidate{}
	// .env files: one flat section
	if len(sections) == 1 && sections[""] != nil {
		m := sections[""]
		if hasAny(m, "aws_access_key_id", "AWS_ACCESS_KEY_ID", "access_key_id", "accessKeyId") {
			delete(sections, "")
			sections[defaultEnvName(m)] = m
		}
	}
	names := make([]string, 0, len(sections))
	for n := range sections {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		sec := sections[n]
		typ, ok := importableSection(sec)
		if !ok {
			continue
		}
		src := sourceFromFlat(typ, importSourceName(n), sec)
		out = append(out, a.candOf(src, filepath.Base(origin)))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no importable credentials found", filepath.Base(origin))
	}
	return out, nil
}

func defaultEnvName(m map[string]string) string {
	if n := firstVal(m, "name", "profile", "AWS_PROFILE"); n != "" {
		return n
	}
	if e := firstVal(m, "endpoint", "endpoint_url", "endpoint_url", "ENDPOINT", "AWS_ENDPOINT_URL"); e != "" {
		return hostLabel(e)
	}
	return "env"
}

// parseGenericINI splits [section] k = v files (AWS and rclone style).
func parseGenericINI(text string) map[string]map[string]string {
	out := map[string]map[string]string{}
	cur := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			cur = strings.TrimSpace(line[1 : len(line)-1])
			cur = strings.TrimPrefix(cur, "profile ")
			if out[cur] == nil {
				out[cur] = map[string]string{}
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if out[cur] == nil {
			out[cur] = map[string]string{}
		}
		out[cur][strings.TrimSpace(k)] = unquote(strings.TrimSpace(v))
	}
	return out
}

func unquote(v string) string {
	if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
		return v[1 : len(v)-1]
	}
	return v
}

// importableSection decides the source type of an INI section: rclone's
// `type =` key wins; aws_* keys mean S3; host+user means sftp.
func importableSection(sec map[string]string) (string, bool) {
	if t := strings.ToLower(firstVal(sec, "type", "kind")); t != "" {
		switch t {
		case profile.TypeS3, profile.TypeSFTP, profile.TypeSCP, profile.TypeFTP,
			profile.TypeFTPS, profile.TypeWebDAV, profile.TypeWebDAVS:
			return t, true
		}
		return "", false
	}
	if hasAny(sec, "aws_access_key_id", "AWS_ACCESS_KEY_ID") {
		return profile.TypeS3, true
	}
	if firstVal(sec, "host", "Host") != "" && firstVal(sec, "user", "username", "User") != "" {
		return profile.TypeSFTP, true
	}
	return "", false
}

// hostLabel turns an endpoint/host string into a compact source name.
func hostLabel(s string) string {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	if i := strings.IndexByte(s, '/'); i > 0 {
		s = s[:i]
		if s == "" {
			return "endpoint"
		}
	}
	return s
}

var credAliases = map[string][]string{
	"access":   {"aws_access_key_id", "AWS_ACCESS_KEY_ID", "accessKeyId", "access_key_id", "accessKey", "awsAccessKeyId"},
	"secret":   {"aws_secret_access_key", "AWS_SECRET_ACCESS_KEY", "secretAccessKey", "secret_access_key", "secretKey", "secret", "pass", "password"},
	"token":    {"aws_session_token", "AWS_SESSION_TOKEN", "sessionToken", "session_token"},
	"endpoint": {"endpoint", "endpoint_url", "endpointUrl", "AWS_ENDPOINT_URL", "s3_endpoint", "url"},
	"region":   {"region", "AWS_REGION", "AWS_DEFAULT_REGION", "defaultRegion"},
	"bucket":   {"bucket", "Bucket"},
	"name":     {"name", "profile", "title"},
	"host":     {"host", "Host", "hostname", "server", "addr"},
	"user":     {"user", "username", "UserName"},
	"port":     {"port", "Port"},
	"root":     {"root", "start_dir", "startDir", "path"},
	"type":     {"type", "kind"},
}

func firstVal(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

func toStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%.0f", t)
	case bool:
		return fmt.Sprintf("%t", t)
	case nil:
		return ""
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func hasAny(m map[string]string, keys ...string) bool {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != "" {
			return true
		}
	}
	return false
}

// sourceFromFlat builds a Source from an INI/env section.
func sourceFromFlat(typ, name string, m map[string]string) profile.Source {
	src := profile.Source{Name: name}
	switch typ {
	case profile.TypeS3:
		src.Type = profile.TypeS3
		src.Bucket = firstVal(m, credAliases["bucket"]...)
		src.S3 = &profile.Profile{
			Name:         name,
			Endpoint:     firstVal(m, credAliases["endpoint"]...),
			Region:       firstVal(m, credAliases["region"]...),
			AccessKeyID:  firstVal(m, credAliases["access"]...),
			SecretKey:    firstVal(m, credAliases["secret"]...),
			SessionToken: firstVal(m, credAliases["token"]...),
		}
	default:
		src.Type = typ
		src.Host = firstVal(m, credAliases["host"]...)
		if p := firstVal(m, credAliases["port"]...); p != "" {
			var port int
			fmt.Sscanf(p, "%d", &port)
			src.Port = port
		}
		src.Username = firstVal(m, credAliases["user"]...)
		src.Password = firstVal(m, credAliases["secret"]...)
		src.Root = firstVal(m, credAliases["root"]...)
		if typ == profile.TypeWebDAV || typ == profile.TypeWebDAVS {
			if u := firstVal(m, "url", "endpoint"); u != "" && src.Host == "" {
				src.Host = u
			}
		}
	}
	return src
}

// candidatesFromPayloads turns KMS/JSON maps into candidates.
func (a *App) candidatesFromPayloads(payloads []map[string]any, origin string) []CredCandidate {
	out := []CredCandidate{}
	for i, p := range payloads {
		flat := map[string]string{}
		for k, v := range p {
			flat[strings.TrimSpace(k)] = toStr(v)
		}
		typ := strings.ToLower(firstVal(flat, credAliases["type"]...))
		if typ == "" {
			if firstVal(flat, credAliases["access"]...) != "" {
				typ = profile.TypeS3
			} else if firstVal(flat, credAliases["host"]...) != "" && firstVal(flat, credAliases["user"]...) != "" {
				typ = profile.TypeSFTP
			} else {
				continue
			}
		}
		name := firstVal(flat, credAliases["name"]...)
		if name == "" {
			name = fmt.Sprintf("%s-%d", origin, i+1)
		}
		src := sourceFromFlat(typ, name, flat)
		if src.Type == profile.TypeS3 && src.S3 != nil && src.S3.Endpoint == "" {
			// single-secret JSON blobs often carry the endpoint as "server"
			src.S3.Endpoint = firstVal(flat, "server", "s3Url")
		}
		out = append(out, a.candOf(src, origin))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (a *App) candOf(src profile.Source, origin string) CredCandidate {
	return CredCandidate{
		ID:        a.stashCred(src),
		Name:      src.Name,
		Type:      src.Type,
		Endpoint:  endpointOf(src),
		Region:    regionOf(src),
		Host:      src.Host,
		Username:  src.Username,
		HasSecret: hasSecret(src),
		Origin:    origin,
	}
}

func endpointOf(s profile.Source) string {
	if s.S3 != nil {
		return s.S3.Endpoint
	}
	return ""
}

func regionOf(s profile.Source) string {
	if s.S3 != nil {
		return s.S3.Region
	}
	return ""
}

func hasSecret(s profile.Source) bool {
	if s.S3 != nil {
		return s.S3.SecretKey != ""
	}
	return s.Password != ""
}

// ---------------------------- KMS fetchers ----------------------------

func kmsHTTP() *http.Client { return &http.Client{Timeout: 20 * time.Second} }

// kmsVault reads one HashiCorp Vault KV v2 secret.
func kmsVault(p map[string]string) ([]map[string]any, error) {
	base := strings.TrimSuffix(p["url"], "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	mount := p["mount"]
	if mount == "" {
		mount = "secret"
	}
	path := strings.TrimPrefix(strings.Trim(p["path"], "/"), mount+"/")
	ver := p["version"]
	u := fmt.Sprintf("%s/%s/data/%s", base, mount, path)
	if ver != "" {
		u += "?version=" + urlQueryEscape(ver)
	}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("X-Vault-Token", p["token"])
	if ns := p["namespace"]; ns != "" {
		req.Header.Set("X-Vault-Namespace", ns)
	}
	body, err := kmsDo(req)
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, fmt.Errorf("unexpected Vault response: %v", err)
	}
	if len(wrap.Data.Data) == 0 {
		return nil, errors.New("secret is empty or not found")
	}
	return []map[string]any{wrap.Data.Data}, nil
}

func urlQueryEscape(s string) string {
	repl := strings.NewReplacer("?", "%3F", "&", "%26", " ", "%20", "+", "%2B")
	return repl.Replace(s)
}

// kmsAwsSecretsManager calls GetSecretValue with hand-rolled SigV4 (no SDK
// dependency needed for one call).
func kmsAwsSecretsManager(p map[string]string) ([]map[string]any, error) {
	region := p["region"]
	if region == "" {
		region = "us-east-1"
	}
	host := fmt.Sprintf("secretsmanager.%s.amazonaws.com", region)
	body := fmt.Sprintf(`{"SecretId":%q}`, p["secretId"])
	body = strings.ReplaceAll(body, "\\", "\\\\")

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	payloadHash := sha256hex([]byte(body))

	canonical := strings.Join([]string{
		"POST", "/",
		"", // no query
		"content-type:application/x-amz-json-1.1",
		"host:" + host,
		"x-amz-date:" + amzDate,
		"x-amz-target:secretsmanager.GetSecretValue",
		"",
		"content-type;host;x-amz-date;x-amz-target",
		payloadHash,
	}, "\n")
	scope := fmt.Sprintf("%s/%s/secretsmanager/aws4_request", dateStamp, region)
	stringToSign := strings.Join([]string{"AWS4-HMAC-SHA256", amzDate, scope, sha256hex([]byte(canonical))}, "\n")

	kDate := hmacSHA256([]byte("AWS4"+p["secretKey"]), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, "secretsmanager")
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := fmt.Sprintf("%x", hmacSHA256(kSigning, stringToSign))

	req, _ := http.NewRequest("POST", "https://"+host+"/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Target", "secretsmanager.GetSecretValue")
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=content-type;host;x-amz-date;x-amz-target, Signature=%s",
		p["accessKey"], scope, signature))

	respBody, err := kmsDo(req)
	if err != nil {
		return nil, err
	}
	var out struct {
		SecretString string `json:"SecretString"`
		Name         string `json:"Name"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, err
	}
	if out.SecretString == "" {
		return nil, errors.New("secret holds no string value")
	}
	payloads := parseSecretString(out.SecretString)
	if len(payloads) > 0 {
		if len(payloads) == 1 {
			if _, ok := payloads[0]["name"]; !ok {
				payloads[0]["name"] = out.Name
			}
		}
		return payloads, nil
	}
	return []map[string]any{{"secret": out.SecretString, "name": out.Name}}, nil
}

// parseSecretString parses a secret string as JSON (object or array) or as
// key=value lines.
func parseSecretString(s string) []map[string]any {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
		var v any
		if err := json.Unmarshal([]byte(s), &v); err == nil {
			return flattenJSON(v)
		}
	}
	// key=value / export key=value lines
	m := map[string]any{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		m[strings.TrimSpace(k)] = unquote(strings.TrimSpace(v))
	}
	if looksLikeCreds(m) {
		return []map[string]any{m}
	}
	return nil
}

// kmsAzureKeyVault: OAuth2 client-credentials → read one secret.
func kmsAzureKeyVault(p map[string]string) ([]map[string]any, error) {
	tenant := p["tenantId"]
	if tenant == "" {
		tenant = p["tenant"]
	}
	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", tenant)
	form := strings.NewReader(urlValues(map[string]string{
		"grant_type":    "client_credentials",
		"client_id":     p["clientId"],
		"client_secret": p["clientSecret"],
		"scope":         "https://vault.azure.net/.default",
	}))
	req, _ := http.NewRequest("POST", tokenURL, form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, err := kmsDo(req)
	if err != nil {
		return nil, fmt.Errorf("azure login failed: %v", err)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return nil, errors.New("azure login failed: no access token")
	}
	vault := strings.TrimSuffix(p["vaultUrl"], "/")
	if vault != "" && !strings.Contains(vault, ".") { // bare vault name
		vault = "https://" + vault + ".vault.azure.net"
	}
	req2, _ := http.NewRequest("GET", vault+"/secrets/"+strings.TrimPrefix(p["secretName"], "/")+"?api-version=7.4", nil)
	req2.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	body2, err := kmsDo(req2)
	if err != nil {
		return nil, err
	}
	var secret struct {
		Value string `json:"value"`
		ID    string `json:"id"`
	}
	if err := json.Unmarshal(body2, &secret); err != nil {
		return nil, err
	}
	if secret.Value == "" {
		return nil, errors.New("secret is empty")
	}
	payloads := parseSecretString(secret.Value)
	if len(payloads) == 0 {
		name := p["secretName"]
		return []map[string]any{{"secret": secret.Value, "name": name}}, nil
	}
	return payloads, nil
}

func urlValues(m map[string]string) string {
	parts := make([]string, 0, len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, urlQueryEscape(k)+"="+urlQueryEscape(m[k]))
	}
	return strings.Join(parts, "&")
}

// kmsGcpSecretManager: service-account JSON → JWT RS256 → access version.
func kmsGcpSecretManager(p map[string]string) ([]map[string]any, error) {
	var sa struct {
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		TokenURI    string `json:"token_uri"`
	}
	if err := json.Unmarshal([]byte(p["credentialsJson"]), &sa); err != nil {
		return nil, fmt.Errorf("credentials JSON invalid: %v", err)
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}
	tok, err := gcpToken(sa.ClientEmail, sa.PrivateKey, sa.TokenURI)
	if err != nil {
		return nil, err
	}
	ver := p["version"]
	if ver == "" {
		ver = "latest"
	}
	u := fmt.Sprintf("https://secretmanager.googleapis.com/v1/projects/%s/secrets/%s/versions/%s:access",
		p["project"], p["secretName"], ver)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	body, err := kmsDo(req)
	if err != nil {
		return nil, err
	}
	var acc struct {
		Payload struct {
			Data string `json:"data"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(body, &acc); err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(acc.Payload.Data)
	if err != nil {
		return nil, err
	}
	payloads := parseSecretString(string(raw))
	if len(payloads) == 0 {
		return nil, errors.New("secret does not contain recognizable credentials")
	}
	return payloads, nil
}

func gcpToken(email, privPEM, tokenURI string) (string, error) {
	block, _ := pem.Decode([]byte(privPEM))
	if block == nil {
		return "", errors.New("private key PEM not found in credentials")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return "", errors.New("private key is not RSA")
	}
	now := time.Now().Unix()
	header := b64url([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{
		"iss":   email,
		"scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud":   tokenURI,
		"iat":   now,
		"exp":   now + 3600,
	})
	signingInput := header + "." + b64url(claims)
	h := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(crand.Reader, key, crypto.SHA256, h[:])
	if err != nil {
		return "", err
	}
	assertion := signingInput + "." + b64url(sig)
	form := strings.NewReader("grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer&assertion=" + urlQueryEscape(assertion))
	req, _ := http.NewRequest("POST", tokenURI, form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, err := kmsDo(req)
	if err != nil {
		return "", err
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return "", errors.New("token exchange failed")
	}
	return tok.AccessToken, nil
}

// kmsCustomHTTP fetches any JSON endpoint (extra headers optional).
func kmsCustomHTTP(p map[string]string) ([]map[string]any, error) {
	method := strings.ToUpper(p["method"])
	if method == "" {
		method = "GET"
	}
	var rdr io.Reader
	if method == "POST" && p["body"] != "" {
		rdr = strings.NewReader(p["body"])
	}
	req, err := http.NewRequest(method, p["url"], rdr)
	if err != nil {
		return nil, err
	}
	if p["token"] != "" {
		req.Header.Set("Authorization", "Bearer "+p["token"])
	}
	for i := 1; i <= 5; i++ {
		if n := p[fmt.Sprintf("headerName%d", i)]; n != "" {
			req.Header.Set(n, p[fmt.Sprintf("headerValue%d", i)])
		}
	}
	body, err := kmsDo(req)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, fmt.Errorf("response is not JSON: %v", err)
	}
	if jp := p["jsonPath"]; jp != "" {
		v = walkJSONPath(v, jp)
	}
	payloads := flattenJSON(v)
	if len(payloads) == 0 {
		return nil, errors.New("no credential objects found in the response")
	}
	return payloads, nil
}

func walkJSONPath(v any, path string) any {
	cur := v
	for _, part := range strings.Split(strings.Trim(path, "/."), ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

func kmsDo(req *http.Request) ([]byte, error) {
	ctx, cancel := context.WithTimeout(req.Context(), 20*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := kmsHTTP().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := string(bytes.TrimSpace(body))
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		return nil, fmt.Errorf("%s: %s", resp.Status, msg)
	}
	return body, nil
}

func sha256hex(b []byte) string {
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h)
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
