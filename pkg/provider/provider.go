// Package provider encodes the quirks of S3-compatible storage providers.
//
// Ported from s3-bucket-tester (https://github.com/MikkoP88/s3-bucket-tester)
// pkg/config/config.go, adapted for use as a standalone capability registry
// that gates admin features and generates user-facing warnings.
package provider

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Capabilities describes what an S3-compatible provider actually supports.
type Capabilities struct {
	Name               string `json:"name"`
	PolicySupport      string `json:"policySupport"` // "Full", "IAM only", "Partial", "None", "Unknown"
	ACLSupport         string `json:"aclSupport"`    // "Full", "Synthetic only", "None", "Unknown"
	VirtualHostSupport bool   `json:"virtualHostSupport"`
	PathStyleSupport   bool   `json:"pathStyleSupport"`
	Notes              string `json:"notes"`
}

// Registry is the curated capability matrix.
var Registry = map[string]Capabilities{
	"aws": {
		Name:               "AWS S3",
		PolicySupport:      "Full",
		ACLSupport:         "Full",
		VirtualHostSupport: true,
		PathStyleSupport:   true,
		Notes:              "Path-style is deprecated but functional",
	},
	"wasabi": {
		Name:               "Wasabi",
		PolicySupport:      "Full",
		ACLSupport:         "Full",
		VirtualHostSupport: true,
		PathStyleSupport:   true,
		Notes:              "Very AWS-compatible",
	},
	"ibm": {
		Name:               "IBM Cloud Object Storage",
		PolicySupport:      "Full",
		ACLSupport:         "Full",
		VirtualHostSupport: true,
		PathStyleSupport:   true,
		Notes:              "Supports both addressing styles",
	},
	"do": {
		Name:               "DigitalOcean Spaces",
		PolicySupport:      "Full",
		ACLSupport:         "Full",
		VirtualHostSupport: true,
		PathStyleSupport:   true,
		Notes:              "AWS-like behavior",
	},
	"b2": {
		Name:               "Backblaze B2 (S3 API)",
		PolicySupport:      "IAM only",
		ACLSupport:         "None",
		VirtualHostSupport: true,
		PathStyleSupport:   true,
		Notes:              "No S3 policy/ACL APIs",
	},
	"minio": {
		Name:               "MinIO / AIStor",
		PolicySupport:      "Full",
		ACLSupport:         "Synthetic only",
		VirtualHostSupport: true,
		PathStyleSupport:   true,
		Notes:              "ACLs are synthetic compatibility shims",
	},
	"cloudflare": {
		Name:               "Cloudflare R2",
		PolicySupport:      "IAM only",
		ACLSupport:         "None",
		VirtualHostSupport: true,
		PathStyleSupport:   false,
		Notes:              "No ACL/policy APIs; path-style not supported",
	},
	"hetzner": {
		Name:               "Hetzner Storage Boxes (S3)",
		PolicySupport:      "Partial",
		ACLSupport:         "None",
		VirtualHostSupport: true,
		PathStyleSupport:   true,
		Notes:              "Basic S3 only",
	},
	"ceph": {
		Name:               "Ceph RGW",
		PolicySupport:      "Full",
		ACLSupport:         "Full",
		VirtualHostSupport: true,
		PathStyleSupport:   true,
		Notes:              "Very complete S3 implementation",
	},
	"dell": {
		Name:               "Dell EMC ECS",
		PolicySupport:      "Full",
		ACLSupport:         "Full",
		VirtualHostSupport: true,
		PathStyleSupport:   true,
		Notes:              "Enterprise S3-compatible",
	},
	"netapp": {
		Name:               "NetApp StorageGRID",
		PolicySupport:      "Full",
		ACLSupport:         "Full",
		VirtualHostSupport: true,
		PathStyleSupport:   true,
		Notes:              "Enterprise S3-compatible",
	},
	"custom": {
		Name:               "Custom/Unknown S3-Compatible",
		PolicySupport:      "Unknown",
		ACLSupport:         "Unknown",
		VirtualHostSupport: false,
		PathStyleSupport:   false,
		Notes:              "Capabilities may vary — consult provider documentation",
	},
}

// Detect returns the provider key for an endpoint URL.
func Detect(endpoint string) string {
	e := strings.ToLower(endpoint)
	switch {
	case strings.Contains(e, "amazonaws.com"):
		return "aws"
	case strings.Contains(e, "wasabisys.com"):
		return "wasabi"
	case strings.Contains(e, "backblazeb2.com"):
		return "b2"
	case strings.Contains(e, "objectstorage.cloud.ibm.com"):
		return "ibm"
	case strings.Contains(e, "digitaloceanspaces.com"):
		return "do"
	case strings.Contains(e, "minio") || strings.Contains(e, "aistor"):
		return "minio"
	case strings.Contains(e, "cloudflare") || strings.Contains(e, "r2"):
		return "cloudflare"
	case strings.Contains(e, "hetzner"):
		return "hetzner"
	case strings.Contains(e, "ceph") || strings.Contains(e, "rgw"):
		return "ceph"
	case strings.Contains(e, "dell") || strings.Contains(e, "ecs"):
		return "dell"
	case strings.Contains(e, "netapp") || strings.Contains(e, "storagegrid"):
		return "netapp"
	case isLoopbackEndpoint(e):
		return "minio" // local lab endpoints are almost always MinIO
	default:
		return "custom"
	}
}

// isLoopbackEndpoint reports whether the endpoint points at localhost.
func isLoopbackEndpoint(e string) bool {
	h := Hostname(e)
	return h == "localhost" || strings.HasSuffix(h, ".localhost") ||
		net.ParseIP(h).IsLoopback()
}

// Get returns capabilities for a provider key (falls back to "custom").
func Get(key string) Capabilities {
	if c, ok := Registry[key]; ok {
		return c
	}
	return Registry["custom"]
}

// NormalizeEndpoint adds the correct scheme to a bare host and strips a
// trailing slash. http:// is used only when explicitly requested because the
// endpoint already carried it or insecure mode is on.
func NormalizeEndpoint(endpoint string, insecure bool) string {
	endpoint = strings.TrimSpace(endpoint)
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return ""
	}
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		if insecure {
			endpoint = "http://" + endpoint
		} else {
			endpoint = "https://" + endpoint
		}
	}
	return endpoint
}

// Hostname extracts the host from an endpoint URL (no scheme/port/path).
func Hostname(endpoint string) string {
	if u, err := url.Parse(NormalizeEndpoint(endpoint, false)); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	// Fallback: manual strip.
	e := strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	if i := strings.Index(e, "/"); i != -1 {
		e = e[:i]
	}
	if i := strings.LastIndex(e, ":"); i != -1 {
		e = e[:i]
	}
	return e
}

// Port extracts the TCP port from an endpoint URL, defaulting by scheme
// (443 for https or bare hosts, 80 for explicit http).
func Port(endpoint string) int {
	normalized := NormalizeEndpoint(endpoint, false)
	if u, err := url.Parse(normalized); err == nil && u.Port() != "" {
		var p int
		if _, err := fmt.Sscanf(u.Port(), "%d", &p); err == nil && p > 0 && p <= 65535 {
			return p
		}
	}
	if strings.HasPrefix(normalized, "http://") {
		return 80
	}
	return 443
}

// InsecureEndpoint reports whether the endpoint explicitly uses plain HTTP.
func InsecureEndpoint(endpoint string) bool {
	return strings.HasPrefix(strings.TrimSpace(endpoint), "http://")
}

// Warnings returns provider-specific warnings for the given configuration.
// Ported from s3-bucket-tester's generateProviderWarnings and extended.
func Warnings(providerKey, endpoint string, pathStyle bool) []string {
	var out []string
	caps := Get(providerKey)

	if pathStyle && !caps.PathStyleSupport {
		if providerKey == "custom" {
			out = append(out, "path-style addressing may not be supported by this provider; try --virtual-hosted")
		} else {
			out = append(out, fmt.Sprintf("path-style addressing is not supported by %s; try --virtual-hosted", caps.Name))
		}
	} else if pathStyle && strings.Contains(caps.Notes, "deprecated") {
		out = append(out, fmt.Sprintf("path-style addressing is deprecated for %s (%s)", caps.Name, caps.Notes))
	}

	if !pathStyle && !caps.VirtualHostSupport && providerKey != "custom" {
		out = append(out, fmt.Sprintf("virtual-hosted addressing may not be supported by %s; try --path-style", caps.Name))
	}

	// Private endpoints (IPs, internal hostnames) usually need path-style.
	host := Hostname(endpoint)
	if net.ParseIP(host) != nil && !pathStyle {
		out = append(out, "endpoint is an IP address: path-style addressing is usually required; add --path-style")
	}

	return out
}

// SupportsPolicy reports whether policy operations are expected to work.
func SupportsPolicy(providerKey string) bool {
	return Get(providerKey).PolicySupport == "Full"
}

// SupportsACL reports whether ACL operations are expected to work.
func SupportsACL(providerKey string) bool {
	s := Get(providerKey).ACLSupport
	return s == "Full" || s == "Synthetic only"
}
