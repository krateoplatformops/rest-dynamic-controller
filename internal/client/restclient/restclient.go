package restclient

import (
	"context"
	"net/http"
	"reflect"
	"strings"

	"fmt"

	unstructuredtools "github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured"
	"github.com/lucasepe/httplib"
	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type UnstructuredClient struct {
	IdentifierFields []string
	SpecFields       *unstructured.Unstructured
	Server           string
	DocScheme        *libopenapi.DocumentModel[v3.Document]
	Auth             httplib.AuthMethod
	Verbose          bool
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

func (u *UnstructuredClient) isIdentifier(field string) bool {
	for _, v := range u.IdentifierFields {
		if v == field {
			return true
		}
	}
	return false
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
}

func (u *UnstructuredClient) Get(ctx context.Context, cli *http.Client, path string, opts *RequestConfiguration) (*map[string]interface{}, error) {
	uri := buildPath(u.Server, path, opts.Parameters, opts.Query)
	pathItem, ok := u.DocScheme.Model.Paths.PathItems.Get(path)
	if !ok {
		return nil, fmt.Errorf("path not found - Get: %s", path)
	}
	if len(pathItem.Get.Servers) > 0 {
		uri = buildPath(pathItem.Get.Servers[0].URL, path, opts.Parameters, opts.Query)
	}

	err := u.ValidateRequest("GET", path, opts.Parameters, opts.Query)
	if err != nil {
		return nil, err
	}
	req, err := httplib.Get(uri.String())
	if err != nil {
		return nil, err
	}

	var val map[string]interface{}
	apiErr := &APIError{}

	httpMethod := "GET"
	getDoc, ok := pathItem.GetOperations().Get(strings.ToLower(httpMethod))
	if !ok {
		return nil, fmt.Errorf("operation not found: %s", httpMethod)
	}

	validStatusCodes, err := getValidResponseCode(getDoc.Responses.Codes)
	if err != nil {
		return nil, err
	}

	var response any
	rh := func(r *http.Response) error {
		if r.ContentLength == 0 {
			return nil
		}
		if r.StatusCode == http.StatusNoContent {
			return nil
		}
		if r.Body == nil {
			return &httplib.StatusError{StatusCode: 404}
		}
		return httplib.FromJSON(&response)(r)
	}

	err = httplib.Fire(cli, req, httplib.FireOptions{
		Verbose:         u.Verbose,
		ResponseHandler: rh,
		AuthMethod:      u.Auth,
		Validators: []httplib.HandleResponseFunc{
			httplib.ErrorJSON(apiErr, validStatusCodes...),
		},
	})
	if err != nil {
		return nil, err
	}
	val, ok = response.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	return &val, nil
}

func (u *UnstructuredClient) Post(ctx context.Context, cli *http.Client, path string, opts *RequestConfiguration) (*map[string]interface{}, error) {
	uri := buildPath(u.Server, path, opts.Parameters, opts.Query)
	pathItem, ok := u.DocScheme.Model.Paths.PathItems.Get(path)
	if !ok {
		return nil, fmt.Errorf("path not found: %s", path)
	}
	if len(pathItem.Post.Servers) > 0 {
		uri = buildPath(pathItem.Post.Servers[0].URL, path, opts.Parameters, opts.Query)
	}

	err := u.ValidateRequest("POST", path, opts.Parameters, opts.Query)
	if err != nil {
		return nil, err
	}

	req, err := httplib.Post(uri.String(), httplib.ToJSON(opts.Body))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")

	var val map[string]interface{}
	apiErr := &APIError{}

	httpMethod := "POST"

	getDoc, ok := pathItem.GetOperations().Get(strings.ToLower(httpMethod))
	if !ok {
		return nil, fmt.Errorf("operation not found: %s", httpMethod)
	}

	validStatusCodes, err := getValidResponseCode(getDoc.Responses.Codes)
	if err != nil {
		return nil, err
	}

	var response any
	rh := func(r *http.Response) error {
		if r.ContentLength == 0 {
			return nil
		}
		return httplib.FromJSON(&response)(r)
	}

	err = httplib.Fire(cli, req, httplib.FireOptions{
		Verbose:         u.Verbose,
		ResponseHandler: rh,
		AuthMethod:      u.Auth,
		Validators: []httplib.HandleResponseFunc{
			httplib.ErrorJSON(apiErr, validStatusCodes...),
		},
	})
	if err != nil {
		return nil, err
	}
	val, ok = response.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	return &val, nil
}

func (u *UnstructuredClient) List(ctx context.Context, cli *http.Client, path string, opts *RequestConfiguration) (*map[string]interface{}, error) {
	uri := buildPath(u.Server, path, opts.Parameters, opts.Query)
	pathItem, ok := u.DocScheme.Model.Paths.PathItems.Get(path)
	if !ok {
		return nil, fmt.Errorf("path not found - list: %s", path)
	}
	if len(pathItem.Get.Servers) > 0 {
		uri = buildPath(pathItem.Get.Servers[0].URL, path, opts.Parameters, opts.Query)
	}

	err := u.ValidateRequest("GET", path, opts.Parameters, opts.Query)
	if err != nil {
		return nil, err
	}
	req, err := httplib.Get(uri.String())
	if err != nil {
		return nil, err
	}

	var val map[string]interface{}
	apiErr := &APIError{}

	httpMethod := "GET"
	getDoc, ok := pathItem.GetOperations().Get(strings.ToLower(httpMethod))
	if !ok {
		return nil, fmt.Errorf("operation not found: %s", httpMethod)
	}

	validStatusCodes, err := getValidResponseCode(getDoc.Responses.Codes)
	if err != nil {
		return nil, err
	}

	var response any
	rh := func(r *http.Response) error {
		if r.ContentLength == 0 {
			return nil
		}
		return httplib.FromJSON(&response)(r)
	}

	err = httplib.Fire(cli, req, httplib.FireOptions{
		Verbose:         u.Verbose,
		ResponseHandler: rh,
		AuthMethod:      u.Auth,
		Validators: []httplib.HandleResponseFunc{
			httplib.ErrorJSON(apiErr, validStatusCodes...),
		},
	})
	if err != nil {
		return nil, err
	}
	val, ok = response.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	return &val, nil
}

func (u *UnstructuredClient) FindBy(ctx context.Context, cli *http.Client, path string, opts *RequestConfiguration) (*map[string]interface{}, error) {
	list, err := u.List(ctx, cli, path, opts)
	if err != nil {
		return nil, err
	}
	for _, v := range *list {
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
	return nil, &httplib.StatusError{StatusCode: 404}
}

func (u *UnstructuredClient) Patch(ctx context.Context, cli *http.Client, path string, opts *RequestConfiguration) (*map[string]interface{}, error) {
	uri := buildPath(u.Server, path, opts.Parameters, opts.Query)
	pathItem, ok := u.DocScheme.Model.Paths.PathItems.Get(path)
	if !ok {
		return nil, fmt.Errorf("path not found: %s", path)
	}
	if len(pathItem.Patch.Servers) > 0 {
		uri = buildPath(pathItem.Patch.Servers[0].URL, path, opts.Parameters, opts.Query)
	}

	err := u.ValidateRequest("PATCH", path, opts.Parameters, opts.Query)
	if err != nil {
		return nil, err
	}

	req, err := httplib.Patch(uri.String(), httplib.ToJSON(opts.Body))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")

	var val map[string]interface{}
	apiErr := &APIError{}

	httpMethod := "PATCH"

	getDoc, ok := pathItem.GetOperations().Get(strings.ToLower(httpMethod))
	if !ok {
		return nil, fmt.Errorf("operation not found: %s", httpMethod)
	}

	validStatusCodes, err := getValidResponseCode(getDoc.Responses.Codes)
	if err != nil {
		return nil, err
	}

	var response any
	rh := func(r *http.Response) error {
		if r.ContentLength == 0 {
			return nil
		}
		return httplib.FromJSON(&response)(r)
	}

	err = httplib.Fire(cli, req, httplib.FireOptions{
		Verbose:         u.Verbose,
		ResponseHandler: rh,
		AuthMethod:      u.Auth,
		Validators: []httplib.HandleResponseFunc{
			httplib.ErrorJSON(apiErr, validStatusCodes...),
		},
	})
	if err != nil {
		return nil, err
	}
	val, ok = response.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	return &val, nil
}

func (u *UnstructuredClient) Put(ctx context.Context, cli *http.Client, path string, opts *RequestConfiguration) (*map[string]interface{}, error) {
	uri := buildPath(u.Server, path, opts.Parameters, opts.Query)
	pathItem, ok := u.DocScheme.Model.Paths.PathItems.Get(path)
	if !ok {
		return nil, fmt.Errorf("path not found: %s", path)
	}
	if len(pathItem.Put.Servers) > 0 {
		uri = buildPath(pathItem.Put.Servers[0].URL, path, opts.Parameters, opts.Query)
	}

	err := u.ValidateRequest("PUT", path, opts.Parameters, opts.Query)
	if err != nil {
		return nil, err
	}

	req, err := httplib.Put(uri.String(), httplib.ToJSON(opts.Body))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")

	var val map[string]interface{}
	apiErr := &APIError{}

	httpMethod := "PUT"
	getDoc, ok := pathItem.GetOperations().Get(strings.ToLower(httpMethod))
	if !ok {
		return nil, fmt.Errorf("operation not found: %s", httpMethod)
	}

	validStatusCodes, err := getValidResponseCode(getDoc.Responses.Codes)
	if err != nil {
		return nil, err
	}

	var response any
	rh := func(r *http.Response) error {
		if r.ContentLength == 0 {
			return nil
		}
		return httplib.FromJSON(&response)(r)
	}

	if containsStatusCode(http.StatusNoContent, validStatusCodes) {
		rh = nil
	}

	err = httplib.Fire(cli, req, httplib.FireOptions{
		Verbose:         u.Verbose,
		ResponseHandler: rh,
		AuthMethod:      u.Auth,
		Validators: []httplib.HandleResponseFunc{
			httplib.ErrorJSON(apiErr, validStatusCodes...),
		},
	})
	if err != nil {
		return nil, err
	}
	val, ok = response.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	return &val, nil
}

func (u *UnstructuredClient) Delete(ctx context.Context, cli *http.Client, path string, opts *RequestConfiguration) (*map[string]interface{}, error) {
	uri := buildPath(u.Server, path, opts.Parameters, opts.Query)
	pathItem, ok := u.DocScheme.Model.Paths.PathItems.Get(path)
	if !ok {
		return nil, fmt.Errorf("path not found: %s", path)
	}
	if len(pathItem.Delete.Servers) > 0 {
		uri = buildPath(pathItem.Delete.Servers[0].URL, path, opts.Parameters, opts.Query)
	}

	err := u.ValidateRequest("DELETE", path, opts.Parameters, opts.Query)
	if err != nil {
		return nil, err
	}
	req, err := httplib.Delete(uri.String())
	if err != nil {
		return nil, err
	}

	var val map[string]interface{}
	apiErr := &APIError{}

	httpMethod := "DELETE"
	getDoc, ok := pathItem.GetOperations().Get(strings.ToLower(httpMethod))
	if !ok {
		return nil, fmt.Errorf("operation not found: %s", httpMethod)
	}

	validStatusCodes, err := getValidResponseCode(getDoc.Responses.Codes)
	if err != nil {
		return nil, err
	}

	var response any
	rh := func(r *http.Response) error {
		if r.ContentLength == 0 {
			return nil
		}
		return httplib.FromJSON(&response)(r)
	}

	err = httplib.Fire(cli, req, httplib.FireOptions{
		Verbose:         u.Verbose,
		ResponseHandler: rh,
		AuthMethod:      u.Auth,
		Validators: []httplib.HandleResponseFunc{
			httplib.ErrorJSON(apiErr, validStatusCodes...),
		},
	})
	if err != nil {
		return nil, err
	}

	val, ok = response.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	return &val, nil
}

func containsStatusCode(statusCode int, validStatusCodes []int) bool {
	for _, v := range validStatusCodes {
		if v == statusCode {
			return true
		}
	}
	return false
}
