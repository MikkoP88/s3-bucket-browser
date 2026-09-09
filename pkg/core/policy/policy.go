// Package policy parses and analyzes bucket policies and ACLs.
//
// The analyzer logic is ported from s3-bucket-tester pkg/checker/policy.go
// (public-access detection via the AllUsers / AuthenticatedUsers group URIs),
// rewritten to consume typed SDK responses instead of raw XML.
package policy

import (
	"encoding/json"
	"fmt"
	"strings"

	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Group URIs that grant access beyond a single account.
const (
	AllUsersURI           = "http://acs.amazonaws.com/groups/global/AllUsers"
	AuthenticatedUsersURI = "http://acs.amazonaws.com/groups/global/AuthenticatedUsers"
)

// Statement is one statement of a bucket policy document.
type Statement struct {
	Sid       string                 `json:"Sid,omitempty"`
	Effect    string                 `json:"Effect"`
	Principal interface{}            `json:"Principal"` // "*", {"AWS": ...}, {"Service": ...}
	Action    interface{}            `json:"Action"`
	Resource  interface{}            `json:"Resource"`
	Condition map[string]interface{} `json:"Condition,omitempty"`
}

// Document is a bucket policy document.
type Document struct {
	Version   string      `json:"Version,omitempty"`
	Id        string      `json:"Id,omitempty"`
	Statement []Statement `json:"Statement"`
}

// PolicySummary is the analyzed view of a bucket policy.
type PolicySummary struct {
	HasPolicy      bool     `json:"hasPolicy"`
	StatementCount int      `json:"statementCount"`
	HasPublicAllow bool     `json:"hasPublicAllow"` // Allow with Principal "*"
	HasPublicRead  bool     `json:"hasPublicRead"`  // public Allow includes GetObject
	HasPublicWrite bool     `json:"hasPublicWrite"` // public Allow includes PutObject/DeleteObject
	AllowedActions []string `json:"allowedActions,omitempty"`
	DeniedActions  []string `json:"deniedActions,omitempty"`
	Principals     []string `json:"principals,omitempty"`
	Resources      []string `json:"resources,omitempty"`
	Conditions     []string `json:"conditions,omitempty"`
	Warnings       []string `json:"warnings,omitempty"`
}

// ParsePolicy validates and parses a policy JSON document.
func ParsePolicy(raw []byte) (*Document, error) {
	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("invalid policy JSON: %w", err)
	}
	if len(doc.Statement) == 0 {
		return nil, fmt.Errorf("policy has no statements")
	}
	return &doc, nil
}

// Analyze inspects a policy document for public access grants.
// Ported from s3-bucket-tester's public-access detection.
func Analyze(doc *Document) PolicySummary {
	s := PolicySummary{HasPolicy: doc != nil}
	if doc == nil {
		return s
	}
	s.StatementCount = len(doc.Statement)

	for _, stmt := range doc.Statement {
		effect := strings.EqualFold(stmt.Effect, "Allow")

		actions := toStrings(stmt.Action)
		for _, a := range actions {
			if effect {
				s.AllowedActions = appendUnique(s.AllowedActions, a)
			} else {
				s.DeniedActions = appendUnique(s.DeniedActions, a)
			}
		}
		s.Resources = append(s.Resources, toStrings(stmt.Resource)...)
		s.Principals = appendUniqueList(s.Principals, principalStrings(stmt.Principal)...)
		for op, v := range stmt.Condition {
			s.Conditions = appendUnique(s.Conditions, fmt.Sprintf("%s: %v", op, v))
		}

		if effect && hasWildcardPrincipal(stmt.Principal) && !conditionRestricts(stmt.Condition) {
			s.HasPublicAllow = true
			for _, a := range actions {
				switch {
				case matchesAction(a, "GetObject"), matchesAction(a, "ListBucket"), a == "s3:*", a == "*":
					s.HasPublicRead = true
				case matchesAction(a, "PutObject"), matchesAction(a, "DeleteObject"), matchesAction(a, "PutBucketPolicy"):
					s.HasPublicWrite = true
				}
			}
		}
	}

	if s.HasPublicRead {
		s.Warnings = append(s.Warnings, "bucket policy grants PUBLIC READ access")
	}
	if s.HasPublicWrite {
		s.Warnings = append(s.Warnings, "bucket policy grants PUBLIC WRITE access")
	}
	return s
}

// ACLSummary is the analyzed view of a bucket ACL.
type ACLSummary struct {
	Owner             string   `json:"owner,omitempty"`
	Grants            []string `json:"grants,omitempty"`
	PublicRead        bool     `json:"publicRead"`
	AuthenticatedRead bool     `json:"authenticatedRead"`
	Warnings          []string `json:"warnings,omitempty"`
}

// AnalyzeACL inspects grants for public access.
// Ported from s3-bucket-tester's ACL public-grant detection.
func AnalyzeACL(owner s3types.Owner, grants []s3types.Grant) ACLSummary {
	s := ACLSummary{}
	if owner.DisplayName != nil {
		s.Owner = *owner.DisplayName
	} else if owner.ID != nil {
		s.Owner = *owner.ID
	}
	for _, g := range grants {
		perm := string(g.Permission)
		uri := ""
		if g.Grantee != nil && g.Grantee.URI != nil {
			uri = *g.Grantee.URI
		}
		switch uri {
		case AllUsersURI:
			s.Grants = append(s.Grants, "Everyone("+strings.ToLower(perm)+")")
			if perm == "READ" || perm == "FULL_CONTROL" {
				s.PublicRead = true
			}
		case AuthenticatedUsersURI:
			s.Grants = append(s.Grants, "AuthenticatedUsers("+strings.ToLower(perm)+")")
			if perm == "READ" || perm == "FULL_CONTROL" {
				s.AuthenticatedRead = true
			}
		default:
			name := uri
			if g.Grantee != nil && g.Grantee.DisplayName != nil {
				name = *g.Grantee.DisplayName
			} else if g.Grantee != nil && g.Grantee.ID != nil {
				name = *g.Grantee.ID
			}
			s.Grants = append(s.Grants, name+"("+strings.ToLower(perm)+")")
		}
	}
	if s.PublicRead {
		s.Warnings = append(s.Warnings, "ACL grants PUBLIC READ access")
	}
	if s.AuthenticatedRead {
		s.Warnings = append(s.Warnings, "ACL grants read access to all authenticated AWS users")
	}
	return s
}

// hasWildcardPrincipal reports whether the principal grants public access:
// a bare "*" or any "*" inside a principal object.
func hasWildcardPrincipal(p interface{}) bool {
	if p == nil {
		return false
	}
	if s, ok := p.(string); ok {
		return s == "*"
	}
	if m, ok := p.(map[string]interface{}); ok {
		for _, v := range m {
			for _, s := range toStrings(v) {
				if s == "*" {
					return true
				}
			}
		}
	}
	return false
}

// principalStrings renders a principal for display: kind=value pairs for
// object principals, or the bare string.
func principalStrings(p interface{}) []string {
	if p == nil {
		return nil
	}
	if s, ok := p.(string); ok {
		return []string{s}
	}
	var out []string
	if m, ok := p.(map[string]interface{}); ok {
		for kind, v := range m {
			for _, s := range toStrings(v) {
				out = appendUnique(out, kind+"="+s)
			}
		}
	}
	return out
}

func appendUniqueList(list []string, vals ...string) []string {
	for _, v := range vals {
		list = appendUnique(list, v)
	}
	return list
}

// conditionRestricts reports whether any condition exists that plausibly
// narrows a wildcard principal (IP condition, etc.). Conservative: any
// condition counts as restricting.
func conditionRestricts(c map[string]interface{}) bool {
	return len(c) > 0
}

// matchesAction reports whether an action ("s3:GetObject", "s3:*", "*")
// covers the wanted verb ("GetObject", "ListBucket", ...).
func matchesAction(action, want string) bool {
	a := strings.TrimPrefix(action, "s3:")
	w := strings.TrimPrefix(want, "s3:")
	if a == w {
		return true
	}
	if a == "*" || a == "*:*" {
		return true
	}
	if strings.HasSuffix(a, "*") && strings.HasPrefix(w, strings.TrimSuffix(a, "*")) {
		return true
	}
	return false
}

// toStrings normalizes a JSON string-or-array value to []string.
func toStrings(v interface{}) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func appendUnique(list []string, v string) []string {
	for _, e := range list {
		if e == v {
			return list
		}
	}
	return append(list, v)
}
