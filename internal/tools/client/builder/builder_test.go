package builder

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/krateoplatformops/rest-dynamic-controller/internal/text"
	restclient "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/client"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/client/apiaction"
	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// mockUnstructuredClient is a mock implementation of the UnstructuredClientInterface for testing.
type mockUnstructuredClient struct {
	requestedParams  map[string]text.StringSet
	requestedQuery   map[string]text.StringSet
	requestedHeaders map[string]text.StringSet
	requestedCookies map[string]text.StringSet
	requestedBody    map[string]text.StringSet
	validateError    error
	paramsError      error
	bodyError        error
}

func (m *mockUnstructuredClient) RequestedParams(method, path string) (text.StringSet, text.StringSet, text.StringSet, text.StringSet, error) {
	if m.paramsError != nil {
		return nil, nil, nil, nil, m.paramsError
	}
	return m.requestedParams[path], m.requestedQuery[path], m.requestedHeaders[path], m.requestedCookies[path], nil
}

func (m *mockUnstructuredClient) RequestedBody(method, path string) (text.StringSet, error) {
	if m.bodyError != nil {
		return nil, m.bodyError
	}
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
	info := &getter.Info{
		Resource: getter.Resource{
			Identifiers: []string{"id"},
			VerbsDescription: []getter.VerbsDescription{
				{Action: "get", Method: "GET", Path: "/test"},
				{Action: "create", Method: "POST", Path: "/test"},
				{Action: "update", Method: "PUT", Path: "/test"},
				{Action: "delete", Method: "DELETE", Path: "/test"},
				{Action: "findBy", Method: "GET", Path: "/test"},
			},
		},
	}

	testCases := []struct {
		name           string
		client         *mockUnstructuredClient
		action         apiaction.APIAction
		expectCallInfo bool
		expectedMethod string
		expectError    bool
		expectedErrMsg string
	}{
		{
			name:   "GET action",
			action: apiaction.Get,
			client: &mockUnstructuredClient{
				requestedParams: map[string]text.StringSet{"/test": text.NewStringSet("id")},
			},
			expectCallInfo: true,
			expectedMethod: "GET",
			expectError:    false,
		},
		{
			name:   "CREATE action",
			action: apiaction.Create,
			client: &mockUnstructuredClient{
				requestedBody: map[string]text.StringSet{"/test": text.NewStringSet("name")},
			},
			expectCallInfo: true,
			expectedMethod: "POST",
			expectError:    false,
		},
		{
			name:   "UPDATE action",
			action: apiaction.Update,
			client: &mockUnstructuredClient{
				requestedBody: map[string]text.StringSet{"/test": text.NewStringSet("name")},
			},
			expectCallInfo: true,
			expectedMethod: "PUT",
			expectError:    false,
		},
		{
			name:           "DELETE action",
			action:         apiaction.Delete,
			client:         &mockUnstructuredClient{},
			expectCallInfo: true,
			expectedMethod: "DELETE",
			expectError:    false,
		},
		{
			name:           "FindBy action",
			action:         apiaction.FindBy,
			client:         &mockUnstructuredClient{},
			expectCallInfo: true,
			expectedMethod: "GET",
			expectError:    false,
		},
		{
			name:           "Unknown action",
			action:         apiaction.APIAction("unknown"),
			client:         &mockUnstructuredClient{},
			expectCallInfo: false,
			expectError:    false,
		},
		{
			name:   "Error from RequestedParams",
			action: apiaction.Get,
			client: &mockUnstructuredClient{
				paramsError: errors.New("client params error"),
			},
			expectCallInfo: false,
			expectError:    true,
			expectedErrMsg: "retrieving requested params: client params error",
		},
		{
			name:   "Error from RequestedBody",
			action: apiaction.Create,
			client: &mockUnstructuredClient{
				bodyError: errors.New("client body error"),
			},
			expectCallInfo: false,
			expectError:    true,
			expectedErrMsg: "retrieving requested body params: client body error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, callInfo, err := APICallBuilder(tc.client, info, tc.action)

			if tc.expectError {
				assert.Error(t, err)
				assert.EqualError(t, err, tc.expectedErrMsg)
			} else {
				assert.NoError(t, err)
			}

			if tc.expectCallInfo {
				assert.NotNil(t, callInfo)
				// apiFunc check is tricky because of function pointers, so we check callInfo
				assert.Equal(t, tc.expectedMethod, callInfo.Method)
			} else {
				assert.Nil(t, callInfo)
			}
		})
	}
}

func TestBuildCallConfig(t *testing.T) {
	baseCallInfo := &CallInfo{
		Path:   "/test/{id}",
		Method: "GET",
		ReqParams: &RequestedParams{
			Parameters: text.NewStringSet("id"),
			Query:      text.NewStringSet("filter"),
			Body:       text.NewStringSet("name"),
		},
		IdentifierFields: []string{"id"},
	}

	baseMg := &unstructured.Unstructured{
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

	testCases := []struct {
		name             string
		callInfo         *CallInfo
		mg               *unstructured.Unstructured
		configSpec       map[string]interface{}
		expectNil        bool
		expectedMethod   string
		expectedParams   map[string]string
		expectedQuery    map[string]string
		expectedBodyKeys []string
	}{
		{
			name:             "Happy path",
			callInfo:         baseCallInfo,
			mg:               baseMg,
			configSpec:       nil,
			expectNil:        false,
			expectedMethod:   "GET",
			expectedParams:   map[string]string{"id": "123"},
			expectedQuery:    map[string]string{"filter": "test"},
			expectedBodyKeys: []string{"name"},
		},
		{
			name:      "Nil callInfo",
			callInfo:  nil,
			mg:        baseMg,
			expectNil: true,
		},
		{
			name:      "Nil managed resource",
			callInfo:  baseCallInfo,
			mg:        nil,
			expectNil: true,
		},
		{
			name:     "Resource with missing status",
			callInfo: baseCallInfo,
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"filter": "test",
						"name":   "testname",
						"id":     "456", // ID is now in spec
					},
				},
			},
			expectNil:        false,
			expectedMethod:   "GET",
			expectedParams:   map[string]string{"id": "456"},
			expectedQuery:    map[string]string{"filter": "test"},
			expectedBodyKeys: []string{"name"},
		},
		{
			name:     "Resource with missing spec",
			callInfo: baseCallInfo,
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"status": map[string]interface{}{
						"id": "123",
					},
				},
			},
			expectNil:        false,
			expectedMethod:   "GET",
			expectedParams:   map[string]string{"id": "123"},
			expectedQuery:    map[string]string{},
			expectedBodyKeys: []string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := BuildCallConfig(tc.callInfo, tc.mg, tc.configSpec)

			if tc.expectNil {
				assert.Nil(t, config)
				return
			}

			assert.NotNil(t, config)
			assert.Equal(t, tc.expectedMethod, config.Method)
			assert.Equal(t, tc.expectedParams, config.Parameters)
			assert.Equal(t, tc.expectedQuery, config.Query)

			bodyMap, ok := config.Body.(map[string]interface{})
			assert.True(t, ok)
			assert.Len(t, bodyMap, len(tc.expectedBodyKeys))
			for _, key := range tc.expectedBodyKeys {
				assert.Contains(t, bodyMap, key)
			}
		})
	}
}

func TestIsResourceKnown(t *testing.T) {
	baseInfo := &getter.Info{
		Resource: getter.Resource{
			Identifiers: []string{"id"},
			VerbsDescription: []getter.VerbsDescription{
				{Action: "get", Method: "GET", Path: "/test"},
			},
		},
	}

	baseMg := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": map[string]interface{}{"id": "123"},
		},
	}

	testCases := []struct {
		name   string
		client restclient.UnstructuredClientInterface
		info   *getter.Info
		mg     *unstructured.Unstructured
		expect bool
	}{
		{
			name: "Happy path, resource is known",
			client: &mockUnstructuredClient{
				requestedParams: map[string]text.StringSet{"/test": text.NewStringSet("id")},
				validateError:   nil,
			},
			info:   baseInfo,
			mg:     baseMg,
			expect: true,
		},
		{
			name:   "Nil clientInfo",
			client: &mockUnstructuredClient{},
			info:   nil,
			mg:     baseMg,
			expect: false,
		},
		{
			name:   "Nil managed resource",
			client: &mockUnstructuredClient{},
			info:   baseInfo,
			mg:     nil,
			expect: false,
		},
		{
			name: "Error from APICallBuilder",
			client: &mockUnstructuredClient{
				paramsError: errors.New("client error"),
			},
			info:   baseInfo,
			mg:     baseMg,
			expect: false,
		},
		{
			name: "Error from ValidateRequest",
			client: &mockUnstructuredClient{
				requestedParams: map[string]text.StringSet{"/test": text.NewStringSet("id")},
				validateError:   errors.New("validation failed"),
			},
			info:   baseInfo,
			mg:     baseMg,
			expect: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := IsResourceKnown(tc.client, tc.info, tc.mg)
			assert.Equal(t, tc.expect, result)
		})
	}
}

func TestProcessFields(t *testing.T) {
	testCases := []struct {
		name           string
		callInfo       *CallInfo
		fields         map[string]interface{}
		expectedParams map[string]string
		expectedQuery  map[string]string
		expectedBody   map[string]interface{}
	}{
		{
			name: "Separate fields for path, query, and body",
			callInfo: &CallInfo{
				ReqParams: &RequestedParams{
					Parameters: text.NewStringSet("id"),
					Query:      text.NewStringSet("filter"),
					Body:       text.NewStringSet("name"),
				},
			},
			fields: map[string]interface{}{
				"id":     "123",
				"filter": "test",
				"name":   "testname",
				"":       "empty", // should be skipped
			},
			expectedParams: map[string]string{"id": "123"},
			expectedQuery:  map[string]string{"filter": "test"},
			expectedBody:   map[string]interface{}{"name": "testname"},
		},
		{
			name: "Field used in Path and Body",
			callInfo: &CallInfo{
				ReqParams: &RequestedParams{
					Parameters: text.NewStringSet("id"),
					Query:      text.NewStringSet("filter"),
					Body:       text.NewStringSet("id", "name"),
				},
			},
			fields: map[string]interface{}{
				"id":   "123",
				"name": "testname",
			},
			expectedParams: map[string]string{"id": "123"},
			expectedQuery:  map[string]string{},
			expectedBody:   map[string]interface{}{"id": "123", "name": "testname"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reqConfig := &restclient.RequestConfiguration{
				Parameters: make(map[string]string),
				Query:      make(map[string]string),
			}
			mapBody := make(map[string]interface{})

			processFields(tc.callInfo, tc.fields, reqConfig, mapBody)

			assert.Equal(t, tc.expectedParams, reqConfig.Parameters)
			assert.Equal(t, tc.expectedQuery, reqConfig.Query)
			assert.Equal(t, tc.expectedBody, mapBody)

			_, exists := reqConfig.Parameters[""]
			assert.False(t, exists, "empty field should not be processed")
		})
	}
}

func TestBuildCallConfig_WithMerge(t *testing.T) {
	testCases := []struct {
		name            string
		callInfo        *CallInfo
		mg              *unstructured.Unstructured
		configSpec      map[string]interface{}
		expectedQuery   map[string]string
		expectedHeaders map[string]string
		expectedBody    map[string]interface{}
	}{
		{
			name: "Values from config and resource are merged correctly",
			callInfo: &CallInfo{
				Action: apiaction.Get,
				ReqParams: &RequestedParams{
					Query:   text.NewStringSet("filter", "api-version"),
					Body:    text.NewStringSet("name", "description"),
					Headers: text.NewStringSet("X-Custom-Header"),
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"filter": "from-resource", // This should override the value from config
						"name":   "from-resource",
					},
				},
			},
			configSpec: map[string]interface{}{
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
			},
			expectedQuery: map[string]string{
				"api-version": "v1-from-config",
				"filter":      "from-resource",
			},
			expectedHeaders: map[string]string{
				"X-Custom-Header": "from-config",
			},
			expectedBody: map[string]interface{}{
				"name": "from-resource",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := BuildCallConfig(tc.callInfo, tc.mg, tc.configSpec)

			assert.NotNil(t, config, "config should not be nil")
			assert.Equal(t, tc.expectedQuery, config.Query)
			assert.Equal(t, tc.expectedHeaders, config.Headers)

			body, ok := config.Body.(map[string]interface{})
			assert.True(t, ok, "body should be a map")
			assert.Equal(t, tc.expectedBody, body)
		})
	}
}
