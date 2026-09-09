package provider

import "testing"

func TestDetect(t *testing.T) {
	cases := map[string]string{
		"https://s3.us-east-1.amazonaws.com":                    "aws",
		"https://bucket.s3.eu-west-1.amazonaws.com":             "aws",
		"https://s3.eu-central-1.wasabisys.com":                 "wasabi",
		"https://s3.us-west-004.backblazeb2.com":                "b2",
		"https://s3.eu-de.cloud-object-storage.appdomain.cloud": "custom",
		"https://s3.us-east.objectstorage.cloud.ibm.com":        "ibm",
		"https://fra1.digitaloceanspaces.com":                   "do",
		"http://localhost:9000":                                 "minio",
		"http://127.0.0.1:9000":                                 "minio",
		"https://minio.example.com":                             "minio",
		"https://accountid.r2.cloudflarestorage.com":            "cloudflare",
		"https://s3.hetzner.com":                                "hetzner",
		"https://ceph-rgw.internal":                             "ceph",
		"https://storagegrid.netapp.local":                      "netapp",
		"https://something.example.org":                         "custom",
	}

	for endpoint, want := range cases {
		if got := Detect(endpoint); got != want {
			t.Errorf("Detect(%q) = %q, want %q", endpoint, got, want)
		}
	}
}

func TestGetFallback(t *testing.T) {
	if got := Get("nope"); got.Name != Registry["custom"].Name {
		t.Errorf("Get(unknown) should fall back to custom, got %q", got.Name)
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	cases := []struct {
		in, want string
		insecure bool
	}{
		{"s3.amazonaws.com", "https://s3.amazonaws.com", false},
		{"s3.amazonaws.com", "http://s3.amazonaws.com", true},
		{"https://x.com/", "https://x.com", false},
		{"  http://y.com  ", "http://y.com", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := NormalizeEndpoint(c.in, c.insecure); got != c.want {
			t.Errorf("NormalizeEndpoint(%q, %v) = %q, want %q", c.in, c.insecure, got, c.want)
		}
	}
}

func TestHostnamePort(t *testing.T) {
	cases := []struct {
		endpoint     string
		host         string
		port         int
		insecureHTTP bool
	}{
		{"https://s3.example.com:8443/path", "s3.example.com", 8443, false},
		{"http://127.0.0.1:9000", "127.0.0.1", 9000, true},
		{"https://plain.example.com", "plain.example.com", 443, false},
		{"http://plain.example.com", "plain.example.com", 80, true},
		{"bare.example.com:9000", "bare.example.com", 9000, false},
	}
	for _, c := range cases {
		if got := Hostname(c.endpoint); got != c.host {
			t.Errorf("Hostname(%q) = %q, want %q", c.endpoint, got, c.host)
		}
		if got := Port(c.endpoint); got != c.port {
			t.Errorf("Port(%q) = %d, want %d", c.endpoint, got, c.port)
		}
		if got := InsecureEndpoint(c.endpoint); got != c.insecureHTTP {
			t.Errorf("InsecureEndpoint(%q) = %v, want %v", c.endpoint, got, c.insecureHTTP)
		}
	}
}

func TestWarnings(t *testing.T) {
	// Cloudflare R2 + path-style must warn.
	if w := Warnings("cloudflare", "https://acct.r2.cloudflarestorage.com", true); len(w) == 0 {
		t.Error("expected path-style warning for R2")
	}
	// AWS + path-style must mention deprecation.
	w := Warnings("aws", "https://s3.us-east-1.amazonaws.com", true)
	if len(w) == 0 {
		t.Error("expected deprecation warning for AWS path-style")
	}
	// IP endpoint without path-style must warn.
	if w := Warnings("custom", "http://10.0.0.5:9000", false); len(w) == 0 {
		t.Error("expected IP endpoint warning recommending path-style")
	}
	// MinIO default config: no warnings.
	if w := Warnings("minio", "https://minio.example.com", false); len(w) != 0 {
		t.Errorf("expected no warnings for minio vhost, got %v", w)
	}
}

func TestSupports(t *testing.T) {
	if !SupportsPolicy("aws") || SupportsPolicy("b2") {
		t.Error("policy support matrix mismatch")
	}
	if !SupportsACL("minio") || SupportsACL("cloudflare") {
		t.Error("ACL support matrix mismatch")
	}
}
