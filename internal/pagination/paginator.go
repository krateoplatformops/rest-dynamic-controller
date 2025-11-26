package pagination

import (
	"fmt"
	"log"
	"net/http"

	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"
)

// Paginator defines the interface for a pagination strategy.
type Paginator interface {
	// Init initializes the paginator for a new sequence of calls.
	Init()

	// UpdateRequest modifies an http.Request with the correct parameters for the current page/token.
	UpdateRequest(req *http.Request) error

	// ShouldContinue determines if another request is needed based on the last response.
	// For instance, it's responsible for extracting the next token/page number and updating its internal state.
	ShouldContinue(resp *http.Response, body []byte) (bool, error)
}

// NewPaginator is a factory that returns the correct paginator based on config.
func NewPaginator(config *getter.Pagination) (Paginator, error) {
	if config == nil {
		log.Printf("In NewPaginator: no pagination config provided")
		return nil, nil // No pagination configured
	}

	switch config.Type {
	case "continuationToken":
		log.Printf("In NewPaginator: creating continuationToken paginator")
		// Ensure that the ContinuationToken config is not nil to avoid panics.
		if config.ContinuationToken == nil {
			return nil, fmt.Errorf("pagination type is 'continuationToken' but the continuationToken config block is missing")
		}
		// Ensure other pagination types block are not set (additional validation).
		//if config.PageNumber != nil {
		//	return nil, fmt.Errorf("pagination type is 'continuationToken' but 'pageNumber' config block is also set")
		//}
		return NewContinuationTokenPaginator(config.ContinuationToken), nil
	// case "pageNumber":
	//     return NewPageNumberPaginator(config.PageNumber), nil
	// other pagination types can be added here
	default:
		return nil, fmt.Errorf("unsupported pagination type: %s", config.Type)
	}
}
