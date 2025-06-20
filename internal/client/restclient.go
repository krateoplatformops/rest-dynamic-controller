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
	return r.statusCode == http.StatusProcessing || r.statusCode == http.StatusContinue || r.statusCode == http.StatusAccepted
}

func (u *UnstructuredClient) Call(ctx context.Context, cli *http.Client, path string, opts *RequestConfiguration) (*Response, error) {
	uri := buildPath(u.Server, path, opts.Parameters, opts.Query)
	pathItem, ok := u.DocScheme.Model.Paths.PathItems.Get(path)
	if !ok {
		return nil, fmt.Errorf("path not found: %s", path)
	}
	httpMethod := string(opts.Method)
	ops := pathItem.GetOperations()
	if ops != nil {
		op, ok := ops.Get(strings.ToLower(httpMethod))
		if !ok {
			return nil, fmt.Errorf("operation not found for method %s at path %s", httpMethod, path)
		}

		if len(op.Servers) > 0 {
			server := op.Servers[0]
			uri = buildPath(server.URL, path, opts.Parameters, opts.Query)
		}
	}

	err := u.ValidateRequest(httpMethod, path, opts.Parameters, opts.Query)
	if err != nil {
		return nil, err
	}

	var response any

	var payload []byte

	headers := make(http.Header)
	payload = nil
	m, ok := opts.Body.(map[string]any)
	if !ok && opts.Body != nil {
		return nil, fmt.Errorf("invalid body type: %T", opts.Body)
	}
	if len(m) != 0 {
		jsonBody, err := json.Marshal(opts.Body)
		if err != nil {
			return nil, err
		}
		payload = jsonBody
		headers.Set("Content-Type", "application/json")
	}

	req := &http.Request{
		Method: httpMethod,
		URL:    uri,
		Proto:  "HTTP/1.1",
		Body:   io.NopCloser(bytes.NewReader(payload)),
		Header: headers,
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
		return nil, fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()

	getDoc, ok := pathItem.GetOperations().Get(strings.ToLower(httpMethod))
	if !ok {
		return nil, fmt.Errorf("operation not found: %s", httpMethod)
	}
	validStatusCodes, err := getValidResponseCode(getDoc.Responses.Codes)
	if err != nil {
		return nil, err
	}

	if !HasValidStatusCode(resp.StatusCode, validStatusCodes...) {
		return nil, &StatusError{
			StatusCode: resp.StatusCode,
			Inner:      fmt.Errorf("invalid status code: %d", resp.StatusCode),
		}
	}

	// Read the response body as we need to check its content length
	// Just checking if resp.Body is nil does not work, as it can be non-nil with a zero-length body.
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	defer resp.Body.Close()

	// Re-wrap body otherwise it will be closed
	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Allow empty body only 204 No Content and 304 Not Modified responses
	statusAllowsEmpty := resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified

	// E.g. 200 but with no content, or 201 Created with no content (error case)
	if len(bodyBytes) == 0 && !statusAllowsEmpty {
		return nil, fmt.Errorf("response body is empty for unexpected status code %d", resp.StatusCode)
	}

	// For status codes that allow empty bodies (e.g., 204, 304), return nil directly, without going through handleResponse
	if len(bodyBytes) == 0 && statusAllowsEmpty {
		return nil, nil
	}

	err = handleResponse(resp.Body, &response)
	if err != nil {
		return nil, fmt.Errorf("error handling response: %w", err)
	}

	return &Response{
		ResponseBody: response,
		statusCode:   resp.StatusCode,
	}, nil
}

// It support both list and single item responses
func (u *UnstructuredClient) FindBy(ctx context.Context, cli *http.Client, path string, opts *RequestConfiguration) (*Response, error) {
	response, err := u.Call(ctx, cli, path, opts)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, nil
	}

	list := response.ResponseBody

	var li map[string]interface{}
	if _, ok := list.([]interface{}); ok {
		li = map[string]interface{}{
			"items": list,
		}
	} else {
		li, ok = list.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected response type: %T", list)
		}
	}

	for _, v := range li {
		if vli, ok := v.([]interface{}); ok {
			if len(vli) > 0 {
				for _, item := range vli {
					itMap, ok := item.(map[string]interface{})
					if !ok {
						continue // skip this item if it's not a map
					}

					for _, ide := range u.IdentifierFields {
						idepath := strings.Split(ide, ".") // split the identifier field by '.'
						responseValue, _, err := unstructured.NestedString(itMap, idepath...)
						if err != nil {
							val, _, err := unstructured.NestedFieldNoCopy(itMap, idepath...)
							if err != nil {
								return nil, fmt.Errorf("error getting nested field: %w", err)
							}
							responseValue = fmt.Sprintf("%v", val)
						}
						ok, err = u.isInSpecFields(ide, responseValue)
						if err != nil {
							return nil, err
						}
						if ok {
							return &Response{
								ResponseBody: itMap,
								statusCode:   response.statusCode,
							}, nil
						}
					}

				}
			}
			break
		}
	}
	return nil, &StatusError{
		StatusCode: http.StatusNotFound,
		Inner:      fmt.Errorf("item not found"),
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
		return fmt.Errorf("error converting JSON to YAML: %w", err)
	}

	err = rawyaml.Unmarshal(yamlData, response)
	if err != nil {
		return fmt.Errorf("error unmarshalling YAML response: %w", err)
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
