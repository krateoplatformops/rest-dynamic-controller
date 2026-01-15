package pagination

import (
	"fmt"
	"net/http"

	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"
)

// continuationTokenPaginator implements the Paginator interface for continuationToken-based pagination.
type continuationTokenPaginator struct {
	config      *getter.ContinuationTokenConfig
	nextToken   string
	isFirstCall bool
}

// NewContinuationTokenPaginator creates a new paginator for the continuation token strategy.
func NewContinuationTokenPaginator(config *getter.ContinuationTokenConfig) Paginator {
	return &continuationTokenPaginator{
		config: config,
	}
}

// Init resets the paginator's state for a new sequence of calls.
func (p *continuationTokenPaginator) Init() {
	//log.Print("Initializing continuationTokenPaginator")
	p.nextToken = ""
	p.isFirstCall = true
}

// UpdateRequest adds the pagination token to the http.Request.
func (p *continuationTokenPaginator) UpdateRequest(req *http.Request) error {
	// Don't add a token on the very first call or if the token is empty.
	if p.isFirstCall || p.nextToken == "" {
		p.isFirstCall = false
		return nil
	}

	cfg := p.config.Request
	switch cfg.TokenIn {
	case "query":
		q := req.URL.Query()
		q.Set(cfg.TokenPath, p.nextToken)
		req.URL.RawQuery = q.Encode()
	case "header":
		req.Header.Set(cfg.TokenPath, p.nextToken)
	default:
		return fmt.Errorf("unsupported tokenIn for request: %s", cfg.TokenIn)
	}

	return nil
}

// ShouldContinue extracts the next token from the response and decides if another call is needed.
func (p *continuationTokenPaginator) ShouldContinue(resp *http.Response, body []byte) (bool, error) {
	cfg := p.config.Response
	var extractedToken string

	switch cfg.TokenIn {
	case "header":
		extractedToken = resp.Header.Get(cfg.TokenPath)
	case "body":
		// Not implemented yet
		//res := gjson.GetBytes(body, cfg.TokenPath)
		//if res.Exists() {
		//	extractedToken = res.String()
		//}
	default:
		return false, fmt.Errorf("unsupported tokenIn for response: %s", cfg.TokenIn)
	}

	// If a new token is found and it's not empty, we should continue.
	if extractedToken != "" {
		p.nextToken = extractedToken
		return true, nil
	}

	// No more tokens, we're done.
	p.nextToken = ""
	return false, nil
}
