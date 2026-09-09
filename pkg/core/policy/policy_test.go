package policy

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestParsePolicy(t *testing.T) {
	if _, err := ParsePolicy([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`)); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
	if _, err := ParsePolicy([]byte(`not json`)); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if _, err := ParsePolicy([]byte(`{"Version":"2012-10-17"}`)); err == nil {
		t.Fatal("policy without statements accepted")
	}
}

func TestAnalyzePublicRead(t *testing.T) {
	doc, err := ParsePolicy([]byte(`{
		"Version": "2012-10-17",
		"Statement": [
			{"Sid":"PublicRead","Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	s := Analyze(doc)
	if !s.HasPublicAllow || !s.HasPublicRead || s.HasPublicWrite {
		t.Errorf("public read not detected: %+v", s)
	}
	if len(s.Warnings) == 0 {
		t.Error("expected public read warning")
	}
}

func TestAnalyzePublicWrite(t *testing.T) {
	doc, _ := ParsePolicy([]byte(`{
		"Statement": [{"Effect":"Allow","Principal":{"AWS":"*"},"Action":["s3:PutObject","s3:DeleteObject"],"Resource":"arn:aws:s3:::b/*"}]
	}`))
	s := Analyze(doc)
	if !s.HasPublicWrite {
		t.Errorf("public write not detected: %+v", s)
	}
}

func TestAnalyzePrivatePolicy(t *testing.T) {
	doc, _ := ParsePolicy([]byte(`{
		"Statement": [{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123:user/u"},"Action":"s3:*","Resource":"arn:aws:s3:::b/*"}]
	}`))
	s := Analyze(doc)
	if s.HasPublicAllow || s.HasPublicRead || s.HasPublicWrite {
		t.Errorf("specific principal flagged as public: %+v", s)
	}
}

func TestAnalyzeConditionedWildcardNotPublic(t *testing.T) {
	doc, _ := ParsePolicy([]byte(`{
		"Statement": [{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*",
			"Condition":{"IpAddress":{"aws:SourceIp":["203.0.113.0/24"]}}}]
	}`))
	s := Analyze(doc)
	if s.HasPublicAllow {
		t.Errorf("conditioned wildcard flagged as public: %+v", s)
	}
}

func TestAnalyzeACL(t *testing.T) {
	grants := []s3types.Grant{
		{Grantee: &s3types.Grantee{Type: s3types.TypeCanonicalUser, ID: aws.String("owner-id")}, Permission: s3types.PermissionFullControl},
		{Grantee: &s3types.Grantee{Type: s3types.TypeGroup, URI: aws.String(AllUsersURI)}, Permission: s3types.PermissionRead},
	}
	s := AnalyzeACL(s3types.Owner{ID: aws.String("owner-id"), DisplayName: aws.String("me")}, grants)
	if !s.PublicRead || s.AuthenticatedRead {
		t.Errorf("ACL analysis wrong: %+v", s)
	}
	if len(s.Warnings) == 0 {
		t.Error("expected ACL warning")
	}

	authOnly := []s3types.Grant{
		{Grantee: &s3types.Grantee{Type: s3types.TypeGroup, URI: aws.String(AuthenticatedUsersURI)}, Permission: s3types.PermissionRead},
	}
	s2 := AnalyzeACL(s3types.Owner{}, authOnly)
	if s2.PublicRead || !s2.AuthenticatedRead {
		t.Errorf("authenticated-users analysis wrong: %+v", s2)
	}

	private := []s3types.Grant{
		{Grantee: &s3types.Grantee{Type: s3types.TypeCanonicalUser, ID: aws.String("x")}, Permission: s3types.PermissionRead},
	}
	s3 := AnalyzeACL(s3types.Owner{}, private)
	if s3.PublicRead || s3.AuthenticatedRead || len(s3.Warnings) != 0 {
		t.Errorf("private ACL flagged: %+v", s3)
	}
}
