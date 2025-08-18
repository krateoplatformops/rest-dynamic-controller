package builder

import (
	"context"
	"net/http"
	"testing"

	"github.com/krateoplatformops/rest-dynamic-controller/internal/text"
	restclient "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/client"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/client/apiaction"
	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type mockUnstructuredClient struct {
	requestedParams  map[string]text.StringSet
	requestedQuery   map[string]text.StringSet
	requestedHeaders map[string]text.StringSet
	requestedCookies map[string]text.StringSet
	requestedBody    map[string]text.StringSet
	validateError    error
}

func (m *mockUnstructuredClient) RequestedParams(method, path string) (text.StringSet, text.StringSet, text.StringSet, text.StringSet, error) {
	return m.requestedParams[path], m.requestedQuery[path], m.requestedHeaders[path], m.requestedCookies[path], nil
}

func (m *mockUnstructuredClient) RequestedBody(method, path string) (text.StringSet, error) {
	return m.requestedBody[path], nil
}

func (m *mockUnstructuredClient) ValidateRequest(method, path string, params, query, headers, cookies map[string]string) error {
	return m.validateError
}

func (m *mockUnstructuredClient) Call(ctx context.Context, cli *http.Client, path string, conf *restclient.RequestConfiguration) (restclient.Response, error) {
	return restclient.Response{}, nil
}

func (m *mockUnstructuredClient) FindBy(ctx context.Context, cli *http.Client, path string, conf *restclient.RequestConfiguration) (restclient.Response, error) {
	return restclient.Response{}, nil
}

func TestAPICallBuilder(t *testing.T) {
	mockClient := &mockUnstructuredClient{
		requestedParams: map[string]text.StringSet{
			"/test": text.NewStringSet("id"),
		},
		requestedQuery: map[string]text.StringSet{
			"/test": text.NewStringSet("filter"),
		},
		requestedBody: map[string]text.StringSet{
			"/test": text.NewStringSet("name"),
		},
	}

	info := &getter.Info{
		Resource: getter.Resource{
			Identifiers: []string{"id"},
			VerbsDescription: []getter.VerbsDescription{
				{
					Action: "get",
					Method: "GET",
					Path:   "/test",
				},
				{
					Action: "create",
					Method: "POST",
					Path:   "/test",
				},
			},
		},
	}

	tests := []struct {
		name           string
		action         apiaction.APIAction
		expectCallInfo bool
		expectError    bool
	}{
		{
			name:           "GET action",
			action:         apiaction.Get,
			expectCallInfo: true,
			expectError:    false,
		},
		{
			name:           "FindBy action",
			action:         apiaction.FindBy,
			expectCallInfo: false,
			expectError:    false,
		},
		{
			name:           "Unknown action",
			action:         apiaction.APIAction("unknown"),
			expectCallInfo: false,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiFunc, callInfo, err := APICallBuilder(mockClient, info, tt.action)

			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.expectCallInfo && callInfo == nil {
				t.Error("expected callInfo but got nil")
			}
			if !tt.expectCallInfo && callInfo != nil && apiFunc != nil {
				t.Error("expected nil callInfo and apiFunc")
			}
		})
	}
}

func TestBuildCallConfig(t *testing.T) {
	callInfo := &CallInfo{
		Path:   "/test/{id}",
		Method: "GET",
		ReqParams: &RequestedParams{
			Parameters: text.NewStringSet("id"),
			Query:      text.NewStringSet("filter"),
			Body:       text.NewStringSet("name"),
		},
		IdentifierFields: []string{"id"},
	}

	mg := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": map[string]interface{}{
				"id":     "123",
				"status": "active",
			},
			"spec": map[string]interface{}{
				"filter": "test",
				"name":   "testname",
			},
		},
	}

	config := BuildCallConfig(callInfo, mg, nil)

	if config == nil {
		t.Fatal("expected config but got nil")
	}

	if config.Method != "GET" {
		t.Errorf("expected method GET, got %s", config.Method)
	}

	if config.Parameters["id"] != "123" {
		t.Errorf("expected parameter id=123, got %s", config.Parameters["id"])
	}

	if config.Query["filter"] != "test" {
		t.Errorf("expected query filter=test, got %s", config.Query["filter"])
	}

	if config.Body == nil {
		t.Fatal("expected body but got nil")
	}

	bMap, ok := config.Body.(map[string]interface{})
	if !ok {
		t.Fatal("expected body to be a map")
	}

	if bMap["name"] != "testname" {
		t.Errorf("expected body name=testname, got %v", bMap["name"])
	}
}

func TestIsResourceKnown(t *testing.T) {
	mockClient := &mockUnstructuredClient{
		requestedParams: map[string]text.StringSet{
			"/test": text.NewStringSet("id"),
		},
		requestedQuery: map[string]text.StringSet{
			"/test": text.NewStringSet(),
		},
		validateError: nil,
	}

	info := &getter.Info{
		Resource: getter.Resource{
			Identifiers: []string{"id"},
			VerbsDescription: []getter.VerbsDescription{
				{
					Action: "get",
					Method: "GET",
					Path:   "/test",
				},
			},
		},
	}

	mg := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": map[string]interface{}{
				"id": "123",
			},
			"spec": map[string]interface{}{},
		},
	}

	result := IsResourceKnown(mockClient, info, mg)

	if !result {
		t.Error("expected resource to be known")
	}
}

func TestProcessFields(t *testing.T) {
	callInfo := &CallInfo{
		ReqParams: &RequestedParams{
			Parameters: text.NewStringSet("id"),
			Query:      text.NewStringSet("filter"),
			Body:       text.NewStringSet("name"),
		},
	}

	reqConfig := &restclient.RequestConfiguration{
		Parameters: make(map[string]string),
		Query:      make(map[string]string),
	}

	mapBody := make(map[string]interface{})

	fields := map[string]interface{}{
		"id":     "123",
		"filter": "test",
		"name":   "testname",
		"":       "empty", // should be skipped
	}

	processFields(callInfo, fields, reqConfig, mapBody)

	if reqConfig.Parameters["id"] != "123" {
		t.Errorf("expected parameter id=123, got %s", reqConfig.Parameters["id"])
	}

	if reqConfig.Query["filter"] != "test" {
		t.Errorf("expected query filter=test, got %s", reqConfig.Query["filter"])
	}

	if mapBody["name"] != "testname" {
		t.Errorf("expected body name=testname, got %v", mapBody["name"])
	}

	if _, exists := reqConfig.Parameters[""]; exists {
		t.Error("empty field should not be processed")
	}
}

func TestBuildCallConfig_WithMerge(t *testing.T) {
	callInfo := &CallInfo{
		Action: apiaction.Get,
		ReqParams: &RequestedParams{
			Query:   text.NewStringSet("filter", "api-version"),
			Body:    text.NewStringSet("name", "description"),
			Headers: text.NewStringSet("X-Custom-Header"),
		},
	}

	mg := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"filter": "from-resource",
				"name":   "from-resource",
			},
		},
	}

	configSpec := map[string]interface{}{
		"query": map[string]interface{}{
			"get": map[string]interface{}{
				"api-version": "v1-from-config",
				"filter":      "from-config",
			},
		},
		"headers": map[string]interface{}{
			"get": map[string]interface{}{
				"X-Custom-Header": "from-config",
			},
		},
	}

	config := BuildCallConfig(callInfo, mg, configSpec)

	assert.NotNil(t, config, "config should not be nil")

	assert.Equal(t, "v1-from-config", config.Query["api-version"], "expected api-version to be from config")
	assert.Equal(t, "from-resource", config.Query["filter"], "expected filter to be from resource (override)")

	assert.Equal(t, "from-config", config.Headers["X-Custom-Header"], "expected header to be from config")

	body, ok := config.Body.(map[string]interface{})
	assert.True(t, ok, "body should be a map")

	assert.Equal(t, "from-resource", body["name"], "expected name to be from resource (override)")
	assert.Nil(t, body["description"], "expected description to be nil as it's not in the resource spec")
}
