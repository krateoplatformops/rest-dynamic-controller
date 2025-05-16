package restclient

import (
	"fmt"
	"net/url"
	"reflect"
	"testing"
)

func TestBuildPath_Basic(t *testing.T) {
	baseUrl := "https://api.example.com/v1"
	path := "/users/{userId}/posts/{postId}"
	parameters := map[string]string{
		"userId": "42",
		"postId": "99",
	}
	query := map[string]string{
		"sort": "desc",
		"page": "2",
	}

	got := buildPath(baseUrl, path, parameters, query)
	if got == nil {
		t.Fatalf("buildPath returned nil")
	}

	got, err := url.Parse(got.String())
	if err != nil {
		t.Fatalf("failed to parse base URL: %v", err)
	}

	expectedPath := "/v1/users/42/posts/99"
	if got.Path != "/v1/users/42/posts/99" {
		t.Errorf("expected path %q, got %q", expectedPath, got.Path)
	}

	wantQuery := url.Values{"sort": {"desc"}, "page": {"2"}}.Encode()
	if got.RawQuery != wantQuery && got.RawQuery != "page=2&sort=desc" {
		t.Errorf("expected query %q, got %q", wantQuery, got.RawQuery)
	}
}

func TestBuildPath_WithPathParamsWithSlashes(t *testing.T) {
	baseUrl := "https://api.example.com/v1"
	path := "/users/{userId}/posts/{postId}"
	parameters := map[string]string{
		"userId": "42/123",
		"postId": "99",
	}
	query := map[string]string{}

	got := buildPath(baseUrl, path, parameters, query)
	if got == nil {
		t.Fatalf("buildPath returned nil")
	}

	fmt.Println("Got Path:", got.RawQuery)

	wantPath := baseUrl + "/users/42%2F123/posts/99"
	if got.String() != wantPath {
		t.Errorf("expected path %q, got %q", wantPath, got.String())
	}
}

func TestBuildPath_NoParams(t *testing.T) {
	baseUrl := "https://api.example.com"
	path := "/status"
	parameters := map[string]string{}
	query := map[string]string{}

	got := buildPath(baseUrl, path, parameters, query)
	if got == nil {
		t.Fatalf("buildPath returned nil")
	}
	got, err := url.Parse(got.String())
	if err != nil {
		t.Fatalf("failed to parse base URL: %v", err)
	}

	wantPath := "/status"
	if got.Path != wantPath {
		t.Errorf("expected path %q, got %q", wantPath, got.Path)
	}
	if got.RawQuery != "" {
		t.Errorf("expected empty query, got %q", got.RawQuery)
	}
}

func TestBuildPath_InvalidBaseURL(t *testing.T) {
	baseUrl := "://bad-url"
	path := "/foo"
	parameters := map[string]string{}
	query := map[string]string{}

	got := buildPath(baseUrl, path, parameters, query)
	if got != nil {
		t.Errorf("expected nil for invalid baseUrl, got %v", got)
	}
}

func TestBuildPath_MultipleQueryParams(t *testing.T) {
	baseUrl := "https://api.example.com"
	path := "/search"
	parameters := map[string]string{}
	query := map[string]string{
		"q":    "golang",
		"lang": "en",
	}

	got := buildPath(baseUrl, path, parameters, query)
	if got == nil {
		t.Fatalf("buildPath returned nil")
	}

	got, err := url.Parse(got.String())
	if err != nil {
		t.Fatalf("failed to parse base URL: %v", err)
	}

	wantPath := "/search"
	if got.Path != wantPath {
		t.Errorf("expected path %q, got %q", wantPath, got.Path)
	}

	parsedQuery, _ := url.ParseQuery(got.RawQuery)
	expectedQuery := url.Values{"q": {"golang"}, "lang": {"en"}}
	if !reflect.DeepEqual(parsedQuery, expectedQuery) {
		t.Errorf("expected query %v, got %v", expectedQuery, parsedQuery)
	}
}

func TestBuildPath_PathWithNoLeadingSlash(t *testing.T) {
	baseUrl := "https://api.example.com/api"
	path := "foo/bar"
	parameters := map[string]string{}
	query := map[string]string{}

	got := buildPath(baseUrl, path, parameters, query)
	if got == nil {
		t.Fatalf("buildPath returned nil")
	}

	got, err := url.Parse(got.String())
	if err != nil {
		t.Fatalf("failed to parse base URL: %v", err)
	}

	wantPath := "/api/foo/bar"
	if got.Path != wantPath {
		t.Errorf("expected path %q, got %q", wantPath, got.Path)
	}
}
