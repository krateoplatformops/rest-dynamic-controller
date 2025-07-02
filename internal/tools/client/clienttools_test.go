package restclient

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/pb33f/libopenapi"
	orderedmap "github.com/pb33f/libopenapi/orderedmap"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
func TestUnstructuredClient_isInResource(t *testing.T) {
	tests := []struct {
		name    string
		client  *UnstructuredClient
		value   string
		fields  []string
		want    bool
		wantErr bool
		errMsg  string
	}{
		{
			name: "nil resource",
			client: &UnstructuredClient{
				Resource: nil,
			},
			value:   "test",
			fields:  []string{"field"},
			want:    false,
			wantErr: true,
			errMsg:  "resource is nil",
		},
		{
			name: "value found in spec",
			client: &UnstructuredClient{
				Resource: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"field": "test-value",
						},
					},
				},
			},
			value:   "test-value",
			fields:  []string{"field"},
			want:    true,
			wantErr: false,
		},
		{
			name: "value found in nested spec field",
			client: &UnstructuredClient{
				Resource: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"nested": map[string]interface{}{
								"field": "nested-value",
							},
						},
					},
				},
			},
			value:   "nested-value",
			fields:  []string{"nested", "field"},
			want:    true,
			wantErr: false,
		},
		{
			name: "value not found in spec, found in status",
			client: &UnstructuredClient{
				Resource: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"field": "spec-value",
						},
						"status": map[string]interface{}{
							"field": "status-value",
						},
					},
				},
			},
			value:   "status-value",
			fields:  []string{"field"},
			want:    true,
			wantErr: false,
		},
		{
			name: "value not found in spec or status",
			client: &UnstructuredClient{
				Resource: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"field": "spec-value",
						},
						"status": map[string]interface{}{
							"field": "status-value",
						},
					},
				},
			},
			value:   "missing-value",
			fields:  []string{"field"},
			want:    false,
			wantErr: false,
		},
		{
			name: "field not found in spec, no status",
			client: &UnstructuredClient{
				Resource: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"otherField": "value",
						},
					},
				},
			},
			value:   "test-value",
			fields:  []string{"field"},
			want:    false,
			wantErr: false,
		},
		{
			name: "field not found in spec or status",
			client: &UnstructuredClient{
				Resource: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"otherField": "value",
						},
						"status": map[string]interface{}{
							"otherField": "status-value",
						},
					},
				},
			},
			value:   "test-value",
			fields:  []string{"field"},
			want:    false,
			wantErr: false,
		},
		{
			name: "empty fields slice",
			client: &UnstructuredClient{
				Resource: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"field": "value",
						},
					},
				},
			},
			value:   "test-value",
			fields:  []string{},
			want:    false,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.client.isInResource(tt.value, tt.fields...)

			if tt.wantErr {
				if err == nil {
					t.Errorf("isInResource() expected error but got none")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("isInResource() error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("isInResource() unexpected error = %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("isInResource() = %v, want %v", got, tt.want)
			}
		})
	}
}
func TestUnstructuredClient_RequestedBody(t *testing.T) {
	tests := []struct {
		name       string
		client     *UnstructuredClient
		httpMethod string
		path       string
		want       []string
		wantErr    bool
		errMsg     string
	}{
		{
			name: "nil doc scheme",
			client: &UnstructuredClient{
				DocScheme: nil,
			},
			httpMethod: "GET",
			path:       "/test",
			want:       nil,
			wantErr:    true,
		},
		{
			name: "path not found",
			client: &UnstructuredClient{
				DocScheme: &libopenapi.DocumentModel[v3.Document]{
					Model: v3.Document{
						Paths: &v3.Paths{
							PathItems: orderedmap.New[string, *v3.PathItem](),
						},
					},
				},
			},
			httpMethod: "GET",
			path:       "/nonexistent",
			want:       nil,
			wantErr:    true,
			errMsg:     "path not found: /nonexistent",
		},
		{
			name: "operation not found",
			client: &UnstructuredClient{
				DocScheme: &libopenapi.DocumentModel[v3.Document]{
					Model: v3.Document{
						Paths: &v3.Paths{
							PathItems: func() *orderedmap.Map[string, *v3.PathItem] {
								m := orderedmap.New[string, *v3.PathItem]()
								pathItem := &v3.PathItem{
									Get:  nil,
									Post: nil,
								}
								m.Set("/test", pathItem)
								return m
							}(),
						},
					},
				},
			},
			httpMethod: "DELETE",
			path:       "/test",
			want:       nil,
			wantErr:    true,
			errMsg:     "operation not found: DELETE",
		},
		{
			name: "no request body",
			client: &UnstructuredClient{
				DocScheme: &libopenapi.DocumentModel[v3.Document]{
					Model: v3.Document{
						Paths: &v3.Paths{
							PathItems: func() *orderedmap.Map[string, *v3.PathItem] {
								m := orderedmap.New[string, *v3.PathItem]()
								pathItem := &v3.PathItem{
									Get: &v3.Operation{
										RequestBody: nil,
									},
								}
								m.Set("/test", pathItem)
								return m
							}(),
						},
					},
				},
			},
			httpMethod: "GET",
			path:       "/test",
			want:       nil,
			wantErr:    false,
		},
		{
			name: "no application/json content",
			client: &UnstructuredClient{
				DocScheme: &libopenapi.DocumentModel[v3.Document]{
					Model: v3.Document{
						Paths: &v3.Paths{
							PathItems: func() *orderedmap.Map[string, *v3.PathItem] {
								m := orderedmap.New[string, *v3.PathItem]()
								content := orderedmap.New[string, *v3.MediaType]()
								content.Set("text/plain", &v3.MediaType{})
								pathItem := &v3.PathItem{
									Post: &v3.Operation{
										RequestBody: &v3.RequestBody{
											Content: content,
										},
									},
								}
								m.Set("/test", pathItem)
								return m
							}(),
						},
					},
				},
			},
			httpMethod: "POST",
			path:       "/test",
			want:       []string{},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.client.RequestedBody(tt.httpMethod, tt.path)

			if tt.wantErr {
				if err == nil {
					t.Errorf("RequestedBody() expected error but got none")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("RequestedBody() error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("RequestedBody() unexpected error = %v", err)
				return
			}

			if tt.want == nil && got != nil {
				t.Errorf("RequestedBody() = %v, want nil", got)
				return
			}

			if tt.want != nil && got == nil {
				t.Errorf("RequestedBody() = nil, want %v", tt.want)
				return
			}

			if got != nil {
				if got == nil {
					t.Errorf("RequestedBody() returned nil, want %v", tt.want)
					return
				}

				for _, wantItem := range tt.want {
					if !got.Contains(wantItem) {
						t.Errorf("RequestedBody() missing expected item %q", wantItem)
					}
				}
			}
		})
	}
}
