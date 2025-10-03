package restclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"

	rawyaml "gopkg.in/yaml.v3"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

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
	uri := buildPath(u.Server, path, opts.Parameters, opts.Query)
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
			server := op.Servers[0]
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
// It serves as the primary orchestrator for the find operation,
// delegating response parsing and item matching to helper functions.
func (u *UnstructuredClient) FindBy(ctx context.Context, cli *http.Client, path string, opts *RequestConfiguration) (Response, error) {
	// Execute the initial API call.
	response, err := u.Call(ctx, cli, path, opts)
	if err != nil {
		return Response{}, err
	}
	if response.ResponseBody == nil {
		return Response{}, &StatusError{
			StatusCode: http.StatusNotFound,
			Inner:      fmt.Errorf("item not found"),
		}
	}

	// Normalize the API response to get a consistent list of items to search.
	// This handles multiple response shapes (e.g., raw list, wrapped list, single object).
	// Shapes handled:
	// 1. A standard JSON array: `[{"id": 1}, {"id": 2}]`
	// 2. An object wrapping the array: `{"items": [{"id": 1}, {"id": 2}], "count": 2}` Note: we take the first array we find in the object as we don't know the property name in advance.
	// 3. A single object, for endpoints that don't use an array for single-item results: `{"id": 1}` (e.g. when the collection only has one item at the moment)
	itemList, err := u.extractItemsFromResponse(response.ResponseBody)
	if err != nil {
		return Response{}, err
	}

	// Delegate the search logic to a dedicated helper function.
	if matchedItem, found := u.findItemInList(itemList); found {
		return Response{
			ResponseBody: matchedItem,
			statusCode:   response.statusCode,
		}, nil
	}

	// If no match is found after checking all items, return a Not Found error.
	return Response{}, &StatusError{
		StatusCode: http.StatusNotFound,
		Inner:      fmt.Errorf("item not found"),
	}
}

// extractItemsFromResponse parses the body of an API response and extracts a list of items.
// It is designed to handle three common API response patterns for list operations:
// 1. A standard JSON array: `[{"id": 1}, {"id": 2}]`
// 2. An object wrapping the array: `{"items": [{"id": 1}, {"id": 2}]}`
// 3. A single object, for endpoints that don't use an array for single-item results: `{"id": 1}`
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
		// Wrap it in a slice to create a list of one.
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

		if isMatch {
			// If a match is found, return the item immediately.
			return itemMap, true
		}
	}

	// Return false if no match was found in the entire list.
	return nil, false
}

// isItemMatch checks if a single item (from an API response) matches the local resource
// by comparing all configured identifier fields.
func (u *UnstructuredClient) isItemMatch(itemMap map[string]interface{}) (bool, error) {
	// Normalize the policy string to lowercase
	policy := strings.ToLower(u.IdentifierMatchPolicy)

	// If no identifiers are specified, no match is possible.
	if len(u.IdentifierFields) == 0 {
		return false, nil
	}

	if policy == "or" {
		// OR Logic: Return true on the first successful identifier match.
		for _, ide := range u.IdentifierFields {
			idepath := strings.Split(ide, ".")

			val, found, err := unstructured.NestedFieldNoCopy(itemMap, idepath...)
			if err != nil || !found {
				// If field is not found or there is an error, it's not a match for this identifier, so we continue.
				continue
			}

			ok, err := u.isInResource(val, idepath...)
			if err != nil {
				// A hard error during comparison should be propagated up.
				return false, err
			}

			if ok {
				// On the first match, we can return true.
				return true, nil
			}
		}

		// If the loop completes, no identifiers matched.
		return false, nil
	} else {
		// AND Logic (default): Return false on the first failed match.
		for _, ide := range u.IdentifierFields {
			idepath := strings.Split(ide, ".")

			val, found, err := unstructured.NestedFieldNoCopy(itemMap, idepath...)
			if err != nil || !found {
				// If any identifier is missing, it's not an AND match.
				return false, nil
			}

			ok, err := u.isInResource(val, idepath...)
			if err != nil {
				// A hard error during comparison should be propagated up.
				return false, err
			}
			if !ok {
				// If any identifier does not match, it's not an AND match.
				return false, nil
			}
		}

		// If the loop completes, it means all identifiers matched.
		return true, nil
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
