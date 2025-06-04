package restclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

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
		Server:    "https://example.com",
	}
}

func TestPathValidation(t *testing.T) {
	client := createTestClient(t)

	// This should get errors because the path does not exist in the OpenAPI document
	_, err := client.Call(context.Background(), &http.Client{}, "/api/nonexistent", &RequestConfiguration{Method: "GET"})
	assert.Error(t, err)
}

func TestCallWithRecorder(t *testing.T) {
	tests := []struct {
		name          string
		handler       http.HandlerFunc
		path          string
		opts          *RequestConfiguration
		clientSetup   func(*UnstructuredClient)
		expected      interface{}
		expectedError string
		expectedURL   string
	}{
		{
			name: "path with slash in path parameter",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET", r.Method)
				r.URL, _ = url.Parse(r.URL.String())
				assert.Equal(t, "/api/test/123%2F456", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{"message": "success"})
			},
			path: "/api/test/{id}",
			opts: &RequestConfiguration{
				Method: "GET",
				Parameters: map[string]string{
					"id": "123%2F456",
				},
			},
			expected: map[string]interface{}{"message": "success"},
		},
		{
			name: "successful GET request",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET", r.Method)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{"message": "success"})
			},
			path: "/api/test",
			opts: &RequestConfiguration{
				Method: "GET",
			},
			expected: map[string]interface{}{"message": "success"},
		},
		{
			name: "successful POST request with body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				var body map[string]interface{}
				err := json.NewDecoder(r.Body).Decode(&body)
				require.NoError(t, err)
				assert.Equal(t, "test", body["name"])

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(map[string]interface{}{"id": "123"})
			},
			path: "/api/test",
			opts: &RequestConfiguration{
				Method: "POST",
				Body:   map[string]interface{}{"name": "test"},
			},
			expected: map[string]interface{}{"id": "123"},
		},
		{
			name: "error for invalid status code",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": "invalid request",
				})
			},
			path: "/api/test",
			opts: &RequestConfiguration{
				Method: "GET",
			},
			expectedError: "unexpected status: 400: invalid status code: 400",
		},
		{
			name: "successful GET with basic auth",
			handler: func(w http.ResponseWriter, r *http.Request) {
				username, password, ok := r.BasicAuth()
				assert.True(t, ok)
				assert.Equal(t, "testuser", username)
				assert.Equal(t, "testpass", password)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{"message": "success"})
			},
			path: "/api/test",
			opts: &RequestConfiguration{
				Method: "GET",
			},
			clientSetup: func(c *UnstructuredClient) {
				c.SetAuth = func(req *http.Request) {
					req.SetBasicAuth("testuser", "testpass")
				}
			},
			expected: map[string]interface{}{"message": "success"},
		},
		{
			name: "successful POST with bearer token",
			handler: func(w http.ResponseWriter, r *http.Request) {
				authHeader := r.Header.Get("Authorization")
				assert.Equal(t, "Bearer testtoken123", authHeader)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(map[string]interface{}{"id": "123"})
			},
			path: "/api/test",
			opts: &RequestConfiguration{
				Method: "POST",
				Body:   map[string]interface{}{"name": "test"},
			},
			clientSetup: func(c *UnstructuredClient) {
				c.SetAuth = func(req *http.Request) {
					req.Header.Set("Authorization", "Bearer "+"testtoken123")
				}
			},
			expected: map[string]interface{}{"id": "123"},
		},
		{
			name: "unauthorized with invalid credentials",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": "invalid credentials",
				})
			},
			path: "/api/test",
			opts: &RequestConfiguration{
				Method: "GET",
			},
			clientSetup: func(c *UnstructuredClient) {

				c.SetAuth = func(req *http.Request) {
					req.SetBasicAuth("invaliduser", "invalidpass")
				}

			},
			expectedError: "unexpected status: 401: invalid status code: 401",
		},
		{
			name: "server override in operation",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Verify that the request is using the override server URL
				// The mock transport doesn't actually change the host, but we can verify
				// the path and that the request was made
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, "/api/override", r.URL.Path)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{"message": "override success"})
			},
			path: "/api/override",
			opts: &RequestConfiguration{
				Method: "GET",
			},
			expected:    map[string]interface{}{"message": "override success"},
			expectedURL: "http://override.example.com/api/override",
		},
		{
			name: "error with empty body and 200 status code",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET", r.Method)
				w.WriteHeader(http.StatusOK) // 200 OK
				// No body written, this should cause an error since 200 expects content
			},
			path: "/api/test/{id}",
			opts: &RequestConfiguration{
				Method: "GET",
				Parameters: map[string]string{
					"id": "123",
				},
			},
			expectedError: "response body is empty for unexpected status code 200",
		},
		{
			name: "error with empty body and 201 status code",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "POST", r.Method)
				w.WriteHeader(http.StatusCreated) // 201 Created
				// No body written, this should cause an error since 201 expects content
			},
			path: "/api/test",
			opts: &RequestConfiguration{
				Method: "POST",
				Body:   map[string]interface{}{"name": "test"},
			},
			expectedError: "response body is empty for unexpected status code 201",
		},
		{
			name: "success with empty body and 204 status code",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "PUT", r.Method)
				w.WriteHeader(http.StatusNoContent) // 204 No Content
				// No body written
			},
			path: "/api/test/{id}",
			opts: &RequestConfiguration{
				Method: "PUT",
				Parameters: map[string]string{
					"id": "123",
				},
				Body: map[string]interface{}{"name": "test"},
			},
			expected: nil, // 204 should return nil
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create our mock transport that uses the ResponseRecorder
			mockTransport := &mockTransport{
				handler: tt.handler,
			}

			// If we need to verify the URL (we set the field in the test case), capture it
			if tt.expectedURL != "" {
				mockTransport.capturedURL = ""
			}

			// Create test client
			client := createTestClient(t)
			if tt.clientSetup != nil {
				tt.clientSetup(client)
			}
			testClient := &http.Client{
				Transport: mockTransport,
			}

			// Call the method under test
			result, err := client.Call(context.Background(), testClient, tt.path, tt.opts)

			// Verify the URL if expected, if we set the field in the test case
			if tt.expectedURL != "" {
				assert.Equal(t, tt.expectedURL, mockTransport.capturedURL)
			}

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				return
			}

			require.NoError(t, err)

			// We expect nil for 204 and 304 responses and no error
			if tt.expected == nil {
				assert.Nil(t, result)
				return
			}

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

// mockTransport implements http.RoundTripper using a ResponseRecorder
type mockTransport struct {
	handler     http.HandlerFunc
	capturedURL string
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Capture the full URL for verification
	if m.capturedURL == "" {
		m.capturedURL = req.URL.String()
	}

	// Create a ResponseRecorder
	rr := httptest.NewRecorder()

	// Call our handler with the recorder
	m.handler(rr, req)

	// Return the recorded response
	return rr.Result(), nil
}
