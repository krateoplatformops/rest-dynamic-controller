package restclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"

	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/pagination"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/pathparsing"
	rawyaml "gopkg.in/yaml.v3"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TODO: maybe consider to wrap http.Response in our Response struct to have access to headers, status code, etc.
// In order to have isPending method attached to a complete response object.
type Response struct {
	ResponseBody any
	statusCode   int
}

func (r *Response) IsPending() bool {
	if r == nil {
		return false
	}
	return r.statusCode == http.StatusProcessing || r.statusCode == http.StatusContinue || r.statusCode == http.StatusAccepted
}

func (u *UnstructuredClient) Call(ctx context.Context, cli *http.Client, path string, opts *RequestConfiguration) (Response, error) {
	if u.DocScheme == nil {
		return Response{}, fmt.Errorf("OpenAPI document scheme not initialized")
	}
	uri := buildPath(u.Server, path, opts.Parameters, opts.Query)
	if uri == nil {
		return Response{}, fmt.Errorf("failed to build URI")
	}

	// We must check if there is a server override for this specific operation
	// so we look it up in the OpenAPI.
	pathItem, ok := u.DocScheme.Model.Paths.PathItems.Get(path)
	if !ok {
		return Response{}, fmt.Errorf("path not found: %s", path)
	}
	httpMethod := string(opts.Method)
	ops := pathItem.GetOperations()
	if ops != nil {
		op, ok := ops.Get(strings.ToLower(httpMethod))
		if !ok {
			return Response{}, fmt.Errorf("operation not found for method %s at path %s", httpMethod, path)
		}

		if len(op.Servers) > 0 {
			server := op.Servers[0] // Use the first server defined for the operation (multiple servers per operation are not supported by Rest Dynamic Controller)
			// Changed the uri since we have a server override for this operation
			uri = buildPath(server.URL, path, opts.Parameters, opts.Query)
		}
	}

	err := u.ValidateRequest(httpMethod, path, opts.Parameters, opts.Query, opts.Headers, opts.Cookies)
	if err != nil {
		return Response{}, err
	}

	var response any
	var payload []byte

	headers := make(http.Header)
	payload = nil
	m, ok := opts.Body.(map[string]any)
	if !ok && opts.Body != nil {
		return Response{}, fmt.Errorf("invalid body type: %T", opts.Body)
	}
	if len(m) != 0 {
		jsonBody, err := json.Marshal(opts.Body)
		if err != nil {
			return Response{}, err
		}
		payload = jsonBody
		headers.Set("Content-Type", "application/json")
	}

	for k, v := range opts.Headers {
		headers.Set(k, v)
	}

	req := &http.Request{
		Method: httpMethod,
		URL:    uri,
		Proto:  "HTTP/1.1",
		Body:   io.NopCloser(bytes.NewReader(payload)),
		Header: headers,
	}

	for k, v := range opts.Cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}

	if u.Debug {
		cli.Transport = &debuggingRoundTripper{
			Transport: cli.Transport,
			Out:       os.Stdout,
		}
	}

	if u.SetAuth != nil {
		u.SetAuth(req)
	}

	// TODO: to be re-enabled when libopenapi-validator is stable
	//if u.Validator != nil {
	//	valid, validationErrors := u.Validator.ValidateRequest(req)
	//	if !valid {
	//		log.Println("Request is NOT valid according to OpenAPI specification:")
	//		for _, err := range validationErrors {
	//			log.Println(err.Error())
	//		}
	//		// Returning a generic error as the validation errors are already logged.
	//		return Response{}, fmt.Errorf("request validation failed")
	//	} else {
	//		log.Println("Request is valid according to OpenAPI specification")
	//	}
	//}

	resp, err := cli.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	getDoc, ok := pathItem.GetOperations().Get(strings.ToLower(httpMethod))
	if !ok {
		return Response{}, fmt.Errorf("operation not found: %s", httpMethod)
	}
	validStatusCodes, err := getValidResponseCode(getDoc.Responses.Codes)
	if err != nil {
		return Response{}, err
	}

	if !HasValidStatusCode(resp.StatusCode, validStatusCodes...) {
		return Response{}, &StatusError{
			StatusCode: resp.StatusCode,
			Inner:      fmt.Errorf("invalid status code: %d", resp.StatusCode),
		}
	}

	// Read the response body as we need to check its content length
	// Just checking if resp.Body is nil does not work, as it can be non-nil with a zero-length body.
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("failed to read response body: %w", err)
	}

	defer resp.Body.Close()

	// Re-wrap body otherwise it will be closed
	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Allow empty body only 204 No Content and 304 Not Modified responses
	statusAllowsEmpty := resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified

	// E.g. 200 but with no content, or 201 Created with no content (error case)
	if len(bodyBytes) == 0 && !statusAllowsEmpty {
		return Response{}, fmt.Errorf("response body is empty for unexpected status code %d", resp.StatusCode)
	}

	// For status codes that allow empty bodies (e.g., 204, 304), return nil directly, without going through handleResponse
	if len(bodyBytes) == 0 && statusAllowsEmpty {
		return Response{
			ResponseBody: nil,
			statusCode:   resp.StatusCode,
		}, nil
	}

	err = handleResponse(resp.Body, &response)
	if err != nil {
		return Response{}, fmt.Errorf("handling response: %w", err)
	}

	return Response{
		ResponseBody: response,
		statusCode:   resp.StatusCode,
	}, nil
}

// FindBy locates a specific resource within an API response it retrieves.
// It serves as the primary orchestrator for the `FindBy` action of the Rest Dynamic Controller,
// delegating response parsing and item matching to helper functions: extractItemsFromResponse, findItemInList, and isItemMatch.
func (u *UnstructuredClient) FindBy(ctx context.Context, cli *http.Client, path string, opts *RequestConfiguration, findByAction *getter.VerbsDescription) (Response, error) {
	if findByAction == nil || findByAction.Pagination == nil {
		// No pagination configured, perform a single call.
		log.Println("FindBy - no pagination configured, performing single call")
		return u.CallFindBySingle(ctx, cli, path, opts)
	}

	log.Println("FindBy - pagination configured, performing paginated calls")

	// Set up debug transport once, before pagination starts
	if u.Debug {
		if _, ok := cli.Transport.(*debuggingRoundTripper); !ok {
			log.Println("FindBy - setting up debugging transport for HTTP client")
			cli.Transport = &debuggingRoundTripper{
				Transport: cli.Transport,
				Out:       os.Stdout,
			}
		}
	}

	// Create the paginator based on the configuration (e.g., continuation token).
	paginator, err := pagination.NewPaginator(findByAction.Pagination)
	if err != nil {
		log.Printf("FindBy - failed to create paginator: %v", err)
		return Response{}, fmt.Errorf("failed to create paginator: %w", err)
	}
	if paginator == nil {
		// Paginator factory returned nil, treat as no pagination.
		log.Println("FindBy - paginator is nil, not normal behavior, performing single call as fallback")
		return u.CallFindBySingle(ctx, cli, path, opts)
	}

	paginator.Init()

	counter := 0
	log.Printf("FindBy - starting pagination loop with paginator type: %T", paginator)
	for {
		counter++
		log.Printf("FindBy - pagination loop iteration %d", counter)
		// Build and execute the request with the current paginator configuration (e.g., continuationToken).
		log.Println("FindBy - executing paginated call")
		response, httpResp, err := u.CallForPagination(ctx, cli, path, opts, paginator)
		if err != nil {
			return Response{}, err
		}
		log.Printf("FindBy - received response for pagination iteration %d", counter)

		// Normalize the response to a list of items.
		itemList, err := u.extractItemsFromResponse(response.ResponseBody)
		if err != nil {
			// If extraction fails, we can't continue.
			return Response{}, err
		}

		// Search for a matching item in the current page's results.
		if matchedItem, found := u.findItemInList(itemList); found {
			// Found a match, return it immediately.
			log.Printf("FindBy - found matching item on pagination iteration number: %d", counter)
			return Response{
				ResponseBody: matchedItem,
				statusCode:   response.statusCode,
			}, nil
		}

		// Ask the paginator if we should continue to the next page.
		bodyBytes, _ := json.Marshal(response.ResponseBody) // Marshal body for analysis by paginator
		shouldContinue, err := paginator.ShouldContinue(httpResp, bodyBytes)
		if err != nil {
			return Response{}, fmt.Errorf("error checking pagination continuation: %w", err)
		}

		if !shouldContinue {
			log.Println("FindBy - pagination complete, no more pages to check")
			// Paginator says we are done, break the loop.
			break
		}
	}
	log.Println("FindBy - exited pagination loop without finding a match")

	// If the loop completes without finding a match, return a Not Found error.
	return Response{}, &StatusError{
		StatusCode: http.StatusNotFound,
		Inner:      fmt.Errorf("item not found after checking all pages"),
	}
}

// CallFindBySingle executes a non-paginated FindBy operation.
func (u *UnstructuredClient) CallFindBySingle(ctx context.Context, cli *http.Client, path string, opts *RequestConfiguration) (Response, error) {
	log.Println("singleCallFindBy - executing single call FindBy operation")
	response, err := u.Call(ctx, cli, path, opts)
	if err != nil {
		return Response{}, err
	}
	if response.ResponseBody == nil {
		return Response{}, &StatusError{StatusCode: http.StatusNotFound, Inner: fmt.Errorf("item not found")}
	}

	// Extract the list of items from the response.
	itemList, err := u.extractItemsFromResponse(response.ResponseBody)
	if err != nil {
		return Response{}, err
	}

	// Delegate the search logic to a dedicated helper function.
	if matchedItem, found := u.findItemInList(itemList); found {
		return Response{ResponseBody: matchedItem, statusCode: response.statusCode}, nil
	}

	// If no match is found after checking all items, return a Not Found error.
	return Response{}, &StatusError{StatusCode: http.StatusNotFound, Inner: fmt.Errorf("item not found")}
}

// CallForPagination builds an `http.Request`, lets the paginator update it, executes it, and returns the response.
// TODO: to be refactored to avoid code duplication with Call method.
// Prerequisite for refactor is to change the Response struct to wrap http.Response directly.
// Differences with Call are mainly the paginator usage and the removal of debug transport setup (otherwise it would be set incrementally on each paginated call).
func (u *UnstructuredClient) CallForPagination(ctx context.Context, cli *http.Client, path string, opts *RequestConfiguration, paginator pagination.Paginator) (Response, *http.Response, error) {
	log.Println("Inside executeCallForPagination")
	if u.DocScheme == nil {
		return Response{}, nil, fmt.Errorf("OpenAPI document scheme not initialized")
	}
	uri := buildPath(u.Server, path, opts.Parameters, opts.Query)
	if uri == nil {
		return Response{}, nil, fmt.Errorf("failed to build URI")
	}

	// We must check if there is a server override for this specific operation
	// so we look it up in the OpenAPI.
	pathItem, ok := u.DocScheme.Model.Paths.PathItems.Get(path)
	if !ok {
		return Response{}, nil, fmt.Errorf("path not found: %s", path)
	}
	httpMethod := string(opts.Method)
	ops := pathItem.GetOperations()
	if ops != nil {
		op, ok := ops.Get(strings.ToLower(httpMethod))
		if !ok {
			return Response{}, nil, fmt.Errorf("operation not found for method %s at path %s", httpMethod, path)
		}

		if len(op.Servers) > 0 {
			server := op.Servers[0] // Use the first server defined for the operation (multiple servers per operation are not supported by Rest Dynamic Controller)
			// Changed the uri since we have a server override for this operation
			uri = buildPath(server.URL, path, opts.Parameters, opts.Query)
		}
	}

	err := u.ValidateRequest(httpMethod, path, opts.Parameters, opts.Query, opts.Headers, opts.Cookies)
	if err != nil {
		return Response{}, nil, err
	}

	var response any
	var payload []byte

	headers := make(http.Header)
	payload = nil
	m, ok := opts.Body.(map[string]any)
	if !ok && opts.Body != nil {
		return Response{}, nil, fmt.Errorf("invalid body type: %T", opts.Body)
	}
	if len(m) != 0 {
		jsonBody, err := json.Marshal(opts.Body)
		if err != nil {
			return Response{}, nil, err
		}
		payload = jsonBody
		headers.Set("Content-Type", "application/json")
	}

	for k, v := range opts.Headers {
		headers.Set(k, v)
	}

	req := &http.Request{
		Method: httpMethod,
		URL:    uri,
		Proto:  "HTTP/1.1",
		Body:   io.NopCloser(bytes.NewReader(payload)),
		Header: headers,
	}

	for k, v := range opts.Cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}

	if u.SetAuth != nil {
		u.SetAuth(req)
	}

	// Let the paginator modify the request (e.g., add token).
	if err := paginator.UpdateRequest(req); err != nil {
		return Response{}, nil, fmt.Errorf("paginator failed to update request: %w", err)
	}

	resp, err := cli.Do(req)
	if err != nil {
		return Response{}, nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	getDoc, ok := pathItem.GetOperations().Get(strings.ToLower(httpMethod))
	if !ok {
		return Response{}, nil, fmt.Errorf("operation not found: %s", httpMethod)
	}
	validStatusCodes, err := getValidResponseCode(getDoc.Responses.Codes)
	if err != nil {
		return Response{}, nil, err
	}

	if !HasValidStatusCode(resp.StatusCode, validStatusCodes...) {
		return Response{}, nil, &StatusError{
			StatusCode: resp.StatusCode,
			Inner:      fmt.Errorf("invalid status code: %d", resp.StatusCode),
		}
	}

	// Read the response body as we need to check its content length
	// Just checking if resp.Body is nil does not work, as it can be non-nil with a zero-length body.
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	defer resp.Body.Close()

	// Re-wrap body otherwise it will be closed
	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Allow empty body only 204 No Content and 304 Not Modified responses
	statusAllowsEmpty := resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified

	// E.g. 200 but with no content, or 201 Created with no content (error case)
	if len(bodyBytes) == 0 && !statusAllowsEmpty {
		return Response{}, nil, fmt.Errorf("response body is empty for unexpected status code %d", resp.StatusCode)
	}

	// For status codes that allow empty bodies (e.g., 204, 304), return nil directly, without going through handleResponse
	if len(bodyBytes) == 0 && statusAllowsEmpty {
		return Response{
			ResponseBody: nil,
			statusCode:   resp.StatusCode,
		}, resp, nil
	}

	err = handleResponse(resp.Body, &response)
	if err != nil {
		return Response{}, nil, fmt.Errorf("handling response: %w", err)
	}

	return Response{
		ResponseBody: response,
		statusCode:   resp.StatusCode,
	}, resp, nil
}

// extractItemsFromResponse parses the body of an API response and extracts a list of items.
// It is designed to handle three common API response patterns for list operations:
// 1. A standard JSON array: `[{"id": 1}, {"id": 2}]`. Note: we take the first array we find in the object as we don't know the property name in advance.
// 2. An object wrapping the array: `{"items": [{"id": 1}, {"id": 2}]}`
// 3. A single object, for endpoints that don't use an array for single-item results: `{"id": 1}` (e.g. when the collection only has one item at the moment)
func (u *UnstructuredClient) extractItemsFromResponse(body interface{}) ([]interface{}, error) {
	// Case 1: The body is already a standard list (JSON array).
	if list, ok := body.([]interface{}); ok {
		return list, nil
	}

	// Case 2 and 3: The body is an object (map).
	if body == nil {
		return nil, fmt.Errorf("response body is nil")
	}
	if bodyMap, ok := body.(map[string]interface{}); ok {
		if len(bodyMap) == 0 {
			return []interface{}{}, nil
		}

		// Case 2: The body is an object, which may contain a list.
		// Iterate through its values to find the first one that is a list.
		for _, v := range bodyMap {
			if list, ok := v.([]interface{}); ok {
				return list, nil
			}
		}

		// Case 3: If no list was found inside the object, assume the object
		// itself is the single item we are looking for e.g. `{"id": 1}`.
		// Wrap it in a slice to create a list of one, e.g. `[{"id": 1}]`.
		return []interface{}{bodyMap}, nil
	}

	// If the body is not a list or an object, it's an unexpected type.
	return nil, fmt.Errorf("unexpected response type: %T", body)
}

// findItemInList iterates through a slice of items and checks if any of them
// match the identifiers of the local resource.
func (u *UnstructuredClient) findItemInList(items []interface{}) (map[string]interface{}, bool) {
	if len(items) == 0 {
		return nil, false
	}

	for _, item := range items {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			// Skip any elements in the list that are not JSON objects.
			continue
		}

		// Delegate the matching logic for a single item to a dedicated helper.
		isMatch, err := u.isItemMatch(itemMap)
		if err != nil {
			// If an error occurs during comparison, we cannot consider it a match.
			// For now, we log the error and continue searching.
			// log.Printf("error matching item: %v", err) // Optional: for debugging
			continue
		}

		// If a match is found, return the item immediately.
		if isMatch {
			return itemMap, true
		}
	}

	// Return false if no match was found in the entire list.
	return nil, false
}

// isItemMatch checks if a single item (from an API response) matches the local resource
// by comparing all configured identifier fields.
// The match logic can be either "AND" (all identifiers must match) or "OR" (any identifier matches).
// Default is "OR" if not specified.
func (u *UnstructuredClient) isItemMatch(itemMap map[string]interface{}) (bool, error) {
	policy := strings.ToLower(u.IdentifiersMatchPolicy)
	//log.Printf("isItemMatch - using IdentifiersMatchPolicy: %s", policy)
	if policy == "" || (policy != "and" && policy != "or") {
		policy = "or" // Default to "or" if not specified or invalid
		//log.Printf("isItemMatch - defaulting IdentifiersMatchPolicy to: %s", policy)
	}

	// If no identifiers are specified, no match is possible.
	if len(u.IdentifierFields) == 0 {
		// TODO: probably warning or error log
		//log.Print("isItemMatch - no IdentifierFields specified, cannot perform match\n")
		return false, nil
	}

	switch policy {
	case "and":
		//log.Print("isItemMatch - AND logic\n")
		// AND Logic: Return false on the first failed match.
		for _, ide := range u.IdentifierFields {
			pathSegments, err := pathparsing.ParsePath(ide)
			//log.Printf("Checking identifier: %s", ide)
			if err != nil || len(pathSegments) == 0 {
				continue
			}

			val, found, err := unstructured.NestedFieldNoCopy(itemMap, pathSegments...)
			if err != nil || !found {
				// If any identifier is missing, it's not an AND match.
				return false, nil
			}

			ok, err := u.isInResource(val, pathSegments...)
			if err != nil {
				// A hard error during comparison should be propagated up.
				return false, err
			}
			if !ok {
				// If any identifier does not match, it's not an AND match.
				return false, nil
			}
			//log.Printf("isItemMatch - identifier %s matched", ide)
		}

		// If the loop completes, it means all identifiers matched (AND logic succeeded).
		//log.Print("isItemMatch - AND logic succeeded, all identifiers matched\n")
		return true, nil
	case "or":
		//log.Print("isItemMatch - using OR logic for identifier matching\n")
		// OR Logic (default): Return true on the first successful identifier match.
		for _, ide := range u.IdentifierFields {
			//log.Print("isItemMatch - OR logic\n")
			//log.Printf("Checking identifier: %s", ide)

			pathSegments, err := pathparsing.ParsePath(ide)
			//log.Printf("Parsed path segments: %v", pathSegments)
			if err != nil || len(pathSegments) == 0 {
				continue
			}

			val, found, err := unstructured.NestedFieldNoCopy(itemMap, pathSegments...)
			//log.Printf("isItemMatch - checking identifier %s: value=%v, found=%v, err=%v", ide, val, found, err)
			//log.Print("isItemMatch, after successful check\n")
			if err != nil || !found {
				// If field is not found or there is an error, it's not a match for this identifier, so we continue.
				continue
			}

			ok, err := u.isInResource(val, pathSegments...)
			//log.Printf("isItemMatch - comparison result for identifier %s: ok=%v, err=%v", ide, ok, err)
			if err != nil {
				// A hard error during comparison should be propagated up. // TODO: is this the desired behavior for OR logic?
				return false, err
			}

			if ok {
				// On the first match, we can return true.
				return true, nil
			}
		}

		//log.Print("isItemMatch - no identifiers matched\n")
		// If the loop completes, no identifiers matched (OR logic failed).
		return false, nil
	default:
		//log.Printf("isItemMatch - unknown IdentifiersMatchPolicy: %s", policy)
		return false, fmt.Errorf("unknown identifier match policy: %s", u.IdentifiersMatchPolicy)
	}
}

func jsonToYAML(jsonData []byte) ([]byte, error) {
	// First unmarshal JSON into a generic interface
	var obj interface{}
	if err := json.Unmarshal(jsonData, &obj); err != nil {
		return nil, err
	}

	// Then marshal to YAML
	yamlData, err := rawyaml.Marshal(obj)
	if err != nil {
		return nil, err
	}

	return yamlData, nil
}

// response should be a pointer to the object where the response will be unmarshalled.
func handleResponse(rc io.ReadCloser, response any) error {
	if rc == nil {
		return nil
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}

	yamlData, err := jsonToYAML(data)
	if err != nil {
		return fmt.Errorf("converting JSON to YAML: %w", err)
	}

	err = rawyaml.Unmarshal(yamlData, response)
	if err != nil {
		return fmt.Errorf("unmarshalling YAML response: %w", err)
	}
	return nil
}

type debuggingRoundTripper struct {
	Transport http.RoundTripper
	Out       io.Writer
}

func (d *debuggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	b, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		return nil, err
	}

	d.Out.Write(b)
	d.Out.Write([]byte{'\n'})

	if d.Transport == nil {
		d.Transport = http.DefaultTransport
	}

	resp, err := d.Transport.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	b, err = httputil.DumpResponse(resp, req.URL.Query().Get("watch") != "true")
	if err != nil {
		return nil, err
	}
	d.Out.Write(b)
	d.Out.Write([]byte{'\n'})

	return resp, err
}

// TODO: to be re-enabled when libopenapi-validator is stable
// Validate delegates the request validation to the underlying validator.
//func (u *UnstructuredClient) Validate(req *http.Request) (bool, []error) {
//	if u.Validator == nil {
//		// If no validator is configured, assume the request is valid.
//		return true, nil
//	}
//	return u.Validator.ValidateRequest(req)
//}
