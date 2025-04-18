package restclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestCallWithRecorder(t *testing.T) {
	tests := []struct {
		name          string
		handler       http.HandlerFunc
		path          string
		opts          *RequestConfiguration
		clientSetup   func(*UnstructuredClient)
		expected      interface{}
		expectedError string
	}{
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
				Body:   map[string]string{"name": "test"},
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
			expectedError: "unexpected status code: 400",
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
				Body:   map[string]string{"name": "test"},
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
			expectedError: "unexpected status code: 401",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create our mock transport that uses the ResponseRecorder
			mockTransport := &mockTransport{
				handler: tt.handler,
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

// mockTransport implements http.RoundTripper using a ResponseRecorder
type mockTransport struct {
	handler http.HandlerFunc
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Create a ResponseRecorder
	rr := httptest.NewRecorder()

	// Call our handler with the recorder
	m.handler(rr, req)

	// Return the recorded response
	return rr.Result(), nil
}
