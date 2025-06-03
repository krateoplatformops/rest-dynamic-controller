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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func (u *UnstructuredClient) Call(ctx context.Context, cli *http.Client, path string, opts *RequestConfiguration) (any, error) {
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

	if resp.Body == nil &&
		resp.StatusCode != http.StatusNoContent &&
		resp.StatusCode != http.StatusNotModified {
		return nil, fmt.Errorf("response body is empty for status code %d", resp.StatusCode)
	}

	err = handleResponse(resp.Body, &response)
	if err != nil {
		return nil, fmt.Errorf("error handling response: %w", err)
	}

	val, ok := response.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response type: %T", response)
	}
	return &val, nil
}

func (u *UnstructuredClient) FindBy(ctx context.Context, cli *http.Client, path string, opts *RequestConfiguration) (any, error) {
	list, err := u.Call(ctx, cli, path, opts)
	if err != nil {
		return nil, err
	}
	if list == nil {
		return nil, nil
	}

	var li map[string]interface{}

	if _, ok := list.([]interface{}); !ok {
		li = map[string]interface{}{
			"items": list,
		}
	}

	if _, ok := list.(map[string]interface{}); !ok {
		return nil, fmt.Errorf("unexpected response type: %T", list)
	}

	for _, v := range li {
		if v, ok := v.([]interface{}); ok {
			if len(v) > 0 {
				for _, item := range v {
					if item, ok := item.(map[string]interface{}); ok {
						for _, ide := range u.IdentifierFields {
							idepath := strings.Split(ide, ".") // split the identifier field by '.'
							responseValue, _, err := unstructured.NestedString(item, idepath...)
							if err != nil {
								val, _, err := unstructured.NestedFieldCopy(item, idepath...)
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
								return &item, nil
							}
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

// buildPath constructs the URL path with the given parameters and query.
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
	return json.Unmarshal(data, &response)
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
