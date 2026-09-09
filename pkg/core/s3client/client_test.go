package s3client

import (
	"context"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

func TestNewLocalEndpoint(t *testing.T) {
	p := profile.Profile{
		Name:        "local",
		Endpoint:    "http://localhost:9000",
		AccessKeyID: "minioadmin",
		SecretKey:   "minioadmin",
		PathStyle:   true,
	}
	c, err := New(context.Background(), p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Endpoint != "http://localhost:9000" {
		t.Errorf("Endpoint = %q", c.Endpoint)
	}
	if !c.PathStyle {
		t.Error("path-style should be on")
	}
	if c.ProviderKey != "minio" { // loopback endpoints are assumed to be MinIO labs
		t.Errorf("ProviderKey = %q, want minio", c.ProviderKey)
	}
	if c.Region != "us-east-1" {
		t.Errorf("Region = %q, want us-east-1 default", c.Region)
	}
}

func TestNewOverrides(t *testing.T) {
	p := profile.Profile{
		Name:        "minio",
		Endpoint:    "minio.example.com",
		AccessKeyID: "k",
		SecretKey:   "s",
		PathStyle:   true,
	}
	pathStyleFalse := false
	c, err := New(context.Background(), p, Options{
		EndpointURL: "https://other.example.com",
		Region:      "eu-west-1",
		PathStyle:   &pathStyleFalse,
		Timeout:     5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.Endpoint != "https://other.example.com" {
		t.Errorf("endpoint override not applied: %q", c.Endpoint)
	}
	if c.Region != "eu-west-1" {
		t.Errorf("region override not applied: %q", c.Region)
	}
	if c.PathStyle {
		t.Error("path-style override not applied")
	}
}

func TestNewAWSDefault(t *testing.T) {
	// No endpoint: AWS default resolution, no explicit credentials needed
	// (will use environment/shared config chain at request time).
	c, err := New(context.Background(), profile.Profile{Name: "aws"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Endpoint != "" {
		t.Errorf("Endpoint = %q, want empty (AWS default)", c.Endpoint)
	}
	if c.ProviderKey != "aws" {
		t.Errorf("ProviderKey = %q, want aws", c.ProviderKey)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "a", "b"); got != "a" {
		t.Errorf("firstNonEmpty = %q", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("firstNonEmpty = %q", got)
	}
}
