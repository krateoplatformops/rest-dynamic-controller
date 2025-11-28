package pagination

import (
	"testing"

	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"
	"github.com/stretchr/testify/assert"
)

func TestNewPaginator(t *testing.T) {
	t.Run("NoPagination", func(t *testing.T) {
		paginator, err := NewPaginator(nil)
		assert.NoError(t, err)
		assert.Nil(t, paginator)
	})

	t.Run("UnsupportedPaginationType", func(t *testing.T) {
		config := &getter.Pagination{
			Type: "unsupported",
		}
		paginator, err := NewPaginator(config)
		assert.Error(t, err)
		assert.Nil(t, paginator)
		assert.Equal(t, "unsupported pagination type: unsupported", err.Error())
	})

	t.Run("ContinuationTokenSuccess", func(t *testing.T) {
		config := &getter.Pagination{
			Type: "continuationToken",
			ContinuationToken: &getter.ContinuationTokenConfig{
				Request: getter.ContinuationTokenRequest{
					TokenIn:   "query",
					TokenPath: "continue",
				},
				Response: getter.ContinuationTokenResponse{
					TokenIn:   "body",
					TokenPath: "metadata.continue",
				},
			},
		}
		paginator, err := NewPaginator(config)
		assert.NoError(t, err)
		assert.NotNil(t, paginator)
		_, ok := paginator.(*continuationTokenPaginator)
		assert.True(t, ok)
	})

	t.Run("ContinuationTokenMissingConfig", func(t *testing.T) {
		config := &getter.Pagination{
			Type: "continuationToken",
		}
		paginator, err := NewPaginator(config)
		assert.Error(t, err)
		assert.Nil(t, paginator)
		assert.Equal(t, "pagination type is 'continuationToken' but the continuationToken config block is missing", err.Error())
	})
}
