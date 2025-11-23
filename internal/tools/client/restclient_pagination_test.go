package restclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"
	"github.com/pb33f/libopenapi"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	paginatedOpenAPISpec = `
openapi: 3.0.0
info:
  title: Paginated API
  version: 1.0.0
paths:
  /items:
    get:
      summary: List all items with pagination
      responses:
        '200':
          description: A paginated list of items
          headers:
            X-Next-Token:
              schema:
                type: string
          content:
            application/json:
              schema:
                type: object
                properties:
                  items:
                    type: array
                    items:
                      type: object
                      properties:
                        id:
                          type: string
                        name:
                          type: string
                  nextToken:
                    type: string
`
)

func TestFindBy_Pagination_HeaderToken(t *testing.T) {
	// Mock server that simulates pagination via headers
	page1Response := `{"items": [{"id": "1", "name": "one"}]}`
	page2Response := `{"items": [{"id": "2", "name": "two"}]}`
	
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "page2" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, page2Response)
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Next-Token", "page2")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, page1Response)
		}
	}))
	defer server.Close()

	doc, err := libopenapi.NewDocument([]byte(paginatedOpenAPISpec))
	assert.NoError(t, err)
	v3Doc, errs := doc.BuildV3Model()
	assert.Empty(t, errs)

	client := &UnstructuredClient{
		Server:    server.URL,
		DocScheme: v3Doc,
		Resource: &unstructured.Unstructured{
			Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"name": "two",
				},
			},
		},
		IdentifierFields: []string{"name"},
	}

	findByAction := &getter.VerbsDescription{
		Pagination: &getter.Pagination{
			Type: "continuationToken",
			ContinuationToken: &getter.ContinuationTokenConfig{
				Request: getter.RequestPagination{
					TokenIn:   "query",
					TokenPath: "token",
				},
				Response: getter.ResponsePagination{
					TokenIn:   "header",
					TokenPath: "X-Next-Token",
				},
			},
		},
	}

	opts := &RequestConfiguration{
		Method: http.MethodGet,
	}

	resp, err := client.FindBy(context.Background(), server.Client(), "/items", opts, findByAction)
	assert.NoError(t, err)
	assert.NotNil(t, resp.ResponseBody)

	bodyMap, ok := resp.ResponseBody.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "2", bodyMap["id"])
	assert.Equal(t, "two", bodyMap["name"])
}

func TestFindBy_Pagination_BodyToken(t *testing.T) {
	// Mock server that simulates pagination via body
	page1Response := `{"items": [{"id": "1", "name": "one"}], "nextToken": "page2"}`
	page2Response := `{"items": [{"id": "2", "name": "two"}], "nextToken": ""}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		w.Header().Set("Content-Type", "application/json")
		if token == "page2" {
			fmt.Fprintln(w, page2Response)
		} else {
			fmt.Fprintln(w, page1Response)
		}
	}))
	defer server.Close()

	doc, err := libopenapi.NewDocument([]byte(paginatedOpenAPISpec))
	assert.NoError(t, err)
	v3Doc, errs := doc.BuildV3Model()
	assert.Empty(t, errs)

	client := &UnstructuredClient{
		Server:    server.URL,
		DocScheme: v3Doc,
		Resource: &unstructured.Unstructured{
			Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"name": "two",
				},
			},
		},
		IdentifierFields: []string{"name"},
	}

	findByAction := &getter.VerbsDescription{
		Pagination: &getter.Pagination{
			Type: "continuationToken",
			ContinuationToken: &getter.ContinuationTokenConfig{
				Request: getter.RequestPagination{
					TokenIn:   "query",
					TokenPath: "token",
				},
				Response: getter.ResponsePagination{
					TokenIn:   "body",
					TokenPath: "nextToken",
				},
			},
		},
	}

	opts := &RequestConfiguration{
		Method: http.MethodGet,
	}

	resp, err := client.FindBy(context.Background(), server.Client(), "/items", opts, findByAction)
	assert.NoError(t, err)
	assert.NotNil(t, resp.ResponseBody)

	bodyMap, ok := resp.ResponseBody.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "2", bodyMap["id"])
}

func TestFindBy_NoPagination(t *testing.T) {
	// Mock server
	responsePayload := `[{"id": "1", "name": "one"}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, responsePayload)
	}))
	defer server.Close()

	doc, err := libopenapi.NewDocument([]byte(paginatedOpenAPISpec))
	assert.NoError(t, err)
	v3Doc, errs := doc.BuildV3Model()
	assert.Empty(t, errs)

	client := &UnstructuredClient{
		Server:    server.URL,
		DocScheme: v3Doc,
		Resource: &unstructured.Unstructured{
			Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"name": "one",
				},
			},
		},
		IdentifierFields: []string{"name"},
	}

	// No pagination in FindBy action
	findByAction := &getter.VerbsDescription{}

	opts := &RequestConfiguration{
		Method: http.MethodGet,
	}

	resp, err := client.FindBy(context.Background(), server.Client(), "/items", opts, findByAction)
	assert.NoError(t, err)
	assert.NotNil(t, resp.ResponseBody)

	bodyMap, ok := resp.ResponseBody.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "1", bodyMap["id"])
}
