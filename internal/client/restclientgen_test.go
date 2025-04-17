package restclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/krateoplatformops/snowplow/plumbing/endpoints"
	"github.com/pb33f/libopenapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestClient(t *testing.T) *UnstructuredClient {
	f, err := os.Open("testdata/openapi.yaml")
	require.NoError(t, err)
	defer f.Close()
	b, err := io.ReadAll(f)
	require.NoError(t, err)
	doc, err := libopenapi.NewDocument(b)
	require.NoError(t, err)

	model, errs := doc.BuildV3Model()
	require.Empty(t, errs)

	return &UnstructuredClient{
		DocScheme: model,
		Endpoint: &endpoints.Endpoint{
			ServerURL: "http://api.example.com/v1",
		},
	}
}

func TestCall(t *testing.T) {
	tests := []struct {
		name          string
		setupServer   func() *httptest.Server
		path          string
		opts          *RequestConfiguration
		expected      interface{}
		expectedError string
	}{
		{
			name: "successful GET request",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "GET", r.Method)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{"message": "success"})
				}))
			},
			path: "/api/test",
			opts: &RequestConfiguration{
				Method: "GET",
			},
			expected: map[string]interface{}{"message": "success"},
		},
		{
			name: "successful POST request with body",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "POST", r.Method)
					assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

					var body map[string]interface{}
					err := json.NewDecoder(r.Body).Decode(&body)
					require.NoError(t, err)
					assert.Equal(t, "test", body["name"])

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					json.NewEncoder(w).Encode(map[string]interface{}{"id": "123"})
				}))
			},
			path: "/api/test",
			opts: &RequestConfiguration{
				Method: "POST",
				Body:   map[string]string{"name": "test"},
			},
			expected: map[string]interface{}{"id": "123"},
		},
		{
			name: "GET with path parameters",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "GET", r.Method)
					assert.Contains(t, r.URL.Path, "user123")
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"id":   "user123",
						"name": "Test User",
					})
				}))
			},
			path: "/api/users/{userId}",
			opts: &RequestConfiguration{
				Method: "GET",
				Parameters: map[string]string{
					"userId": "user123",
				},
			},
			expected: map[string]interface{}{
				"id":   "user123",
				"name": "Test User",
			},
		},
		{
			name: "GET with query parameters",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "GET", r.Method)
					assert.Equal(t, "test", r.URL.Query().Get("query"))
					assert.Equal(t, "5", r.URL.Query().Get("limit"))
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode([]interface{}{
						map[string]interface{}{"id": "1", "title": "First"},
						map[string]interface{}{"id": "2", "title": "Second"},
					})
				}))
			},
			path: "/api/search",
			opts: &RequestConfiguration{
				Method: "GET",
				Query: map[string]string{
					"query": "test",
					"limit": "5",
				},
			},
			expected: []interface{}{
				map[string]interface{}{"id": "1", "title": "First"},
				map[string]interface{}{"id": "2", "title": "Second"},
			},
		},
		{
			name: "invalid status code",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusBadRequest)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"error": "invalid request",
					})
				}))
			},
			path: "/api/test",
			opts: &RequestConfiguration{
				Method: "GET",
			},
			expectedError: "unexpected status code: 400",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupServer()
			defer server.Close()

			client := createTestClient(t)
			client.Endpoint.ServerURL = server.URL

			result, err := client.Call(context.Background(), server.Client(), tt.path, tt.opts)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				return
			}

			require.NoError(t, err)

			if result == nil {
				assert.Nil(t, tt.expected)
				return
			}

			switch v := result.(type) {
			case *map[string]interface{}:
				assert.Equal(t, tt.expected, *v)
			case *[]interface{}:
				assert.Equal(t, tt.expected, *v)
			default:
				t.Errorf("unexpected result type: %T", result)
			}
		})
	}
}
