package restclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"

	unstructuredtools "github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured"

	"github.com/krateoplatformops/snowplow/plumbing/endpoints"
	"github.com/krateoplatformops/snowplow/plumbing/http/request"
	"github.com/krateoplatformops/snowplow/plumbing/ptr"
	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type UnstructuredClient struct {
	IdentifierFields []string
	SpecFields       *unstructured.Unstructured
	DocScheme        *libopenapi.DocumentModel[v3.Document]
	Endpoint         *endpoints.Endpoint
}

type APIError struct {
	Message   string `json:"message"`
	TypeKey   string `json:"typeKey"`
	ErrorCode int    `json:"errorCode"`
	EventID   int    `json:"eventId"`
}

type RequestConfiguration struct {
	Parameters map[string]string
	Query      map[string]string
	Body       interface{}
	Method     string
}

// 'field' could be in the format of 'spec.field1.field2'
func (u *UnstructuredClient) isInSpecFields(field, value string) (bool, error) {
	fields := strings.Split(field, ".")
	specs, err := unstructuredtools.GetFieldsFromUnstructured(u.SpecFields, "spec")
	if err != nil {
		return false, fmt.Errorf("error getting fields from unstructured: %w", err)
	}

	val, ok, err := unstructured.NestedFieldCopy(specs, fields...)
	if err != nil {
		return false, fmt.Errorf("error getting nested field: %w", err)
	}
	if !ok {
		return false, nil
	}
	if reflect.DeepEqual(val, value) {
		return true, nil
	}

	return false, nil
}

func (u *UnstructuredClient) Call(ctx context.Context, cli *http.Client, path string, opts *RequestConfiguration) (any, error) {
	uri := buildPath(path, opts.Parameters, opts.Query)
	pathItem, ok := u.DocScheme.Model.Paths.PathItems.Get(path)
	if !ok {
		return nil, fmt.Errorf("path not found: %s", path)
	}
	if len(pathItem.Get.Servers) > 0 {
		u.Endpoint.ServerURL = pathItem.Get.Servers[0].URL
	}
	httpMethod := string(opts.Method)

	err := u.ValidateRequest(httpMethod, path, opts.Parameters, opts.Query)
	if err != nil {
		return nil, err
	}

	var response any

	var payload *string
	var headers []string
	if opts.Body == nil {
		payload = nil
		headers = nil
	} else {
		jsonBody, err := json.Marshal(opts.Body)
		if err != nil {
			return nil, err
		}
		payload = ptr.To(string(jsonBody))
		headers = []string{"Content-Type", "application/json"}
	}
	res := request.Do(ctx, request.RequestOptions{
		Verb:     ptr.To(httpMethod),
		Endpoint: u.Endpoint,
		Headers:  headers,
		Path:     uri.String(),
		Payload:  payload,
		ResponseHandler: func(rc io.ReadCloser) error {
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
		},
	})
	getDoc, ok := pathItem.GetOperations().Get(strings.ToLower(httpMethod))
	if !ok {
		return nil, fmt.Errorf("operation not found: %s", httpMethod)
	}
	validStatusCodes, err := getValidResponseCode(getDoc.Responses.Codes)
	if err != nil {
		return nil, err
	}

	if !HasValidStatusCode(res.Code, validStatusCodes...) {
		return nil, fmt.Errorf("unexpected status code: %d - message: %s - status: %s", res.Code, res.Message, res.Status)
	}

	val, ok := response.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	return &val, nil
}
