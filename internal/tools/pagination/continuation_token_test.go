package pagination

import (
	"net/http"
	"net/http/httptest"
	"testing"

	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"
	"github.com/stretchr/testify/assert"
)

func TestContinuationTokenPaginator(t *testing.T) {
	t.Run("Init", func(t *testing.T) {
		// Pre-set values to check they get reset
		p := &continuationTokenPaginator{
			nextToken:   "some-token",
			isFirstCall: false,
		}
		p.Init()
		assert.Equal(t, "", p.nextToken)
		assert.True(t, p.isFirstCall)
	})

	t.Run("UpdateRequestQueryFirstCall", func(t *testing.T) {
		p := NewContinuationTokenPaginator(&getter.ContinuationTokenConfig{
			Request: getter.ContinuationTokenRequest{
				TokenIn:   "query",
				TokenPath: "continue",
			},
		})
		p.Init()
		req := httptest.NewRequest("GET", "http://example.com", nil)
		err := p.UpdateRequest(req)
		assert.NoError(t, err)
		assert.Equal(t, "", req.URL.Query().Get("continue"))
	})

	t.Run("UpdateRequestQuerySecondCall", func(t *testing.T) {
		p := NewContinuationTokenPaginator(&getter.ContinuationTokenConfig{
			Request: getter.ContinuationTokenRequest{
				TokenIn:   "query",
				TokenPath: "continue",
			},
		})
		p.Init()
		// First call
		req := httptest.NewRequest("GET", "http://example.com", nil)
		err := p.UpdateRequest(req)
		assert.NoError(t, err)
		assert.Equal(t, "", req.URL.Query().Get("continue"))

		// Second call
		cp := p.(*continuationTokenPaginator)
		cp.nextToken = "next-page-token"
		req = httptest.NewRequest("GET", "http://example.com", nil)
		err = p.UpdateRequest(req)
		assert.NoError(t, err)
		assert.Equal(t, "next-page-token", req.URL.Query().Get("continue"))
	})

	t.Run("UpdateRequestHeaderFirstCall", func(t *testing.T) {
		p := NewContinuationTokenPaginator(&getter.ContinuationTokenConfig{
			Request: getter.ContinuationTokenRequest{
				TokenIn:   "header",
				TokenPath: "X-Continue",
			},
		})
		p.Init()
		req := httptest.NewRequest("GET", "http://example.com", nil)
		err := p.UpdateRequest(req)
		assert.NoError(t, err)
		assert.Equal(t, "", req.Header.Get("X-Continue"))
	})

	t.Run("UpdateRequestHeaderSecondCall", func(t *testing.T) {
		p := NewContinuationTokenPaginator(&getter.ContinuationTokenConfig{
			Request: getter.ContinuationTokenRequest{
				TokenIn:   "header",
				TokenPath: "X-Continue",
			},
		})
		p.Init()
		// First call
		req := httptest.NewRequest("GET", "http://example.com", nil)
		err := p.UpdateRequest(req)
		assert.NoError(t, err)

		// Second call
		cp := p.(*continuationTokenPaginator)
		cp.nextToken = "next-page-token"
		req = httptest.NewRequest("GET", "http://example.com", nil)
		err = p.UpdateRequest(req)
		assert.NoError(t, err)
		assert.Equal(t, "next-page-token", req.Header.Get("X-Continue"))
	})

	t.Run("ShouldContinueHeader", func(t *testing.T) {
		p := NewContinuationTokenPaginator(&getter.ContinuationTokenConfig{
			Response: getter.ContinuationTokenResponse{
				TokenIn:   "header",
				TokenPath: "X-Next-Token",
			},
		})
		p.Init()
		resp := &http.Response{
			Header: http.Header{},
		}
		resp.Header.Set("X-Next-Token", "new-token")
		shouldContinue, err := p.ShouldContinue(resp, nil)
		assert.NoError(t, err)
		assert.True(t, shouldContinue)
		cp := p.(*continuationTokenPaginator)
		assert.Equal(t, "new-token", cp.nextToken)
	})

	t.Run("ShouldContinueHeaderNoToken", func(t *testing.T) {
		p := NewContinuationTokenPaginator(&getter.ContinuationTokenConfig{
			Response: getter.ContinuationTokenResponse{
				TokenIn:   "header",
				TokenPath: "X-Next-Token",
			},
		})
		p.Init()
		resp := &http.Response{
			Header: http.Header{},
		}
		shouldContinue, err := p.ShouldContinue(resp, nil)
		assert.NoError(t, err)
		assert.False(t, shouldContinue)
		cp := p.(*continuationTokenPaginator)
		assert.Equal(t, "", cp.nextToken)
	})

	t.Run("ShouldContinueUnsupported", func(t *testing.T) {
		p := NewContinuationTokenPaginator(&getter.ContinuationTokenConfig{
			Response: getter.ContinuationTokenResponse{
				TokenIn: "unsupported",
			},
		})
		p.Init()
		resp := &http.Response{}
		shouldContinue, err := p.ShouldContinue(resp, nil)
		assert.Error(t, err)
		assert.False(t, shouldContinue)
	})
}
