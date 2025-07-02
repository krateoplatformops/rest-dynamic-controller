package restclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	pathutil "path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	stringset "github.com/krateoplatformops/rest-dynamic-controller/internal/text"
	fgetter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/filegetter"
	unstructuredtools "github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	orderedmap "github.com/pb33f/libopenapi/orderedmap"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

type APICallType string

type AuthType string

const (
	AuthTypeBasic  AuthType = "basic"
	AuthTypeBearer AuthType = "bearer"
)

func (a AuthType) String() string {
	return string(a)
}
func ToType(ty string) (AuthType, error) {
	switch ty {
	case "basic":
		return AuthTypeBasic, nil
	case "bearer":
		return AuthTypeBearer, nil
	}
	return "", fmt.Errorf("unknown auth type: %s", ty)
}

func buildPath(baseUrl string, path string, parameters map[string]string, query map[string]string) *url.URL {
	for key, param := range parameters {
		param = url.PathEscape(param)
		path = strings.Replace(path, fmt.Sprintf("{%s}", key), param, 1)
	}

	params := url.Values{}
	for key, param := range query {
		queryParam := url.QueryEscape(param)
		params.Add(key, queryParam)
	}

	parsed, err := url.Parse(baseUrl)
	if err != nil {
		return nil
	}

	// Remove trailing slash from base path if present
	parsed.Opaque = "//" + pathutil.Join(parsed.Host, parsed.Path, path)
	parsed.RawQuery = params.Encode()
	parsed, err = url.Parse(parsed.String())
	if err != nil {
		return nil
	}
	return parsed
}
func getValidResponseCode(codes *orderedmap.Map[string, *v3.Response]) ([]int, error) {
	var validCodes []int
	for code := codes.First(); code != nil; code = code.Next() {
		icode, err := strconv.Atoi(code.Key())
		if err != nil {
			return nil, fmt.Errorf("invalid response code: %s", code.Key())
		}
		if icode >= 200 && icode < 300 {
			validCodes = append(validCodes, icode)
			// return icode, nil
		}
	}
	return validCodes, nil
}

type UnstructuredClientInterface interface {
	ValidateRequest(httpMethod string, path string, parameters map[string]string, query map[string]string) error
	RequestedBody(httpMethod string, path string) (bodys stringset.StringSet, err error)
	RequestedParams(httpMethod string, path string) (parameters stringset.StringSet, query stringset.StringSet, err error)
	FindBy(ctx context.Context, cli *http.Client, path string, conf *RequestConfiguration) (Response, error)
	Call(ctx context.Context, cli *http.Client, path string, conf *RequestConfiguration) (Response, error)
}

type UnstructuredClient struct {
	IdentifierFields []string
	Resource         *unstructured.Unstructured
	DocScheme        *libopenapi.DocumentModel[v3.Document]
	Server           string
	Debug            bool
	SetAuth          func(req *http.Request)
}

type RequestConfiguration struct {
	Parameters map[string]string
	Query      map[string]string
	Body       any
	Method     string
}

func (u *UnstructuredClient) isInResource(value string, fields ...string) (bool, error) {
	if u.Resource == nil {
		return false, fmt.Errorf("resource is nil")
	}
	specs, err := unstructuredtools.GetFieldsFromUnstructured(u.Resource, "spec")
	if err != nil {
		return false, fmt.Errorf("getting fields from unstructured: %w", err)
	}

	val, ok, err := unstructured.NestedFieldCopy(specs, fields...)
	if err != nil {
		return false, fmt.Errorf("getting nested field: %w", err)
	}
	if ok && reflect.DeepEqual(val, value) {
		return true, nil
	}

	// if value is not found in spec, we check the status (if it exists)
	if u.Resource.Object["status"] == nil {
		return false, nil
	}

	status, err := unstructuredtools.GetFieldsFromUnstructured(u.Resource, "status")
	if err != nil {
		return false, fmt.Errorf("getting fields from unstructured: %w", err)
	}

	val, ok, err = unstructured.NestedFieldCopy(status, fields...)
	if err != nil {
		return false, fmt.Errorf("getting nested field: %w", err)
	}
	if !ok {
		return false, nil
	}

	if reflect.DeepEqual(val, value) {
		return true, nil
	}

	// end of the search, if we reach this point, the value is not found
	return false, nil
}

func (u *UnstructuredClient) ValidateRequest(httpMethod string, path string, parameters map[string]string, query map[string]string) error {
	pathItem, ok := u.DocScheme.Model.Paths.PathItems.Get(path)
	if !ok {
		return fmt.Errorf("path not found: %s", path)
	}
	getDoc, ok := pathItem.GetOperations().Get(strings.ToLower(httpMethod))
	if !ok {
		return fmt.Errorf("operation not found: %s", httpMethod)
	}
	for _, param := range getDoc.Parameters {
		if param.Required != nil && *param.Required {
			if param.In == "path" {
				if _, ok := parameters[param.Name]; !ok {
					return fmt.Errorf("missing path parameter: %s", param.Name)
				}
			}
			if param.In == "query" {
				if _, ok := query[param.Name]; !ok {
					return fmt.Errorf("missing query parameter: %s", param.Name)
				}
			}
		}
	}
	return nil
}

// RequestedBody is a method that returns the body parameters for a given HTTP method and path.
func (u *UnstructuredClient) RequestedBody(httpMethod string, path string) (bodyParams stringset.StringSet, err error) {
	if u.DocScheme == nil || u.DocScheme.Model.Paths == nil {
		return nil, fmt.Errorf("document scheme or model is nil")
	}

	pathItem, ok := u.DocScheme.Model.Paths.PathItems.Get(path)
	if !ok {
		return nil, fmt.Errorf("path not found: %s", path)
	}
	getDoc, ok := pathItem.GetOperations().Get(strings.ToLower(httpMethod))
	if !ok {
		return nil, fmt.Errorf("operation not found: %s", httpMethod)
	}
	bodyParams = stringset.NewStringSet()
	if getDoc.RequestBody == nil {
		return nil, nil
	}
	bodySchema, ok := getDoc.RequestBody.Content.Get("application/json")
	if !ok {
		return bodyParams, nil
	}
	schema, err := bodySchema.Schema.BuildSchema()
	if err != nil {
		return nil, fmt.Errorf("building schema for %s: %w", path, err)
	}
	populateFromAllOf(schema)

	for sch := schema.Properties.First(); sch != nil; sch = sch.Next() {
		bodyParams.Add(sch.Key())
	}

	return bodyParams, nil
}

// func PopulateFromAllOf() is a method that populates the schema with the properties from the allOf field.
// the recursive function to populate the schema with the properties from the allOf field.
func populateFromAllOf(schema *base.Schema) {
	if len(schema.Type) > 0 && schema.Type[0] == "array" {
		if schema.Items != nil {
			if schema.Items.N == 0 {
				sch, err := schema.Items.A.BuildSchema()
				if err != nil {
					return
				}

				populateFromAllOf(sch)
			}
		}
		return
	}
	for prop := schema.Properties.First(); prop != nil; prop = prop.Next() {
		populateFromAllOf(prop.Value().Schema())
	}
	for _, proxy := range schema.AllOf {
		propSchema, err := proxy.BuildSchema()
		populateFromAllOf(propSchema)
		if err != nil {
			return
		}
		// Iterate over the properties of the schema with First() and Next()
		for prop := propSchema.Properties.First(); prop != nil; prop = prop.Next() {
			if schema.Properties == nil {
				schema.Properties = orderedmap.New[string, *base.SchemaProxy]()
			}
			// Add the property to the schema
			schema.Properties.Set(prop.Key(), prop.Value())
		}
	}
}

// RequestedParams is a method that returns the parameters and query parameters for a given HTTP method and path.
func (u *UnstructuredClient) RequestedParams(httpMethod string, path string) (parameters stringset.StringSet, query stringset.StringSet, err error) {
	pathItem, ok := u.DocScheme.Model.Paths.PathItems.Get(path)
	if !ok {
		return nil, nil, fmt.Errorf("path not found: %s", path)
	}
	getDoc, ok := pathItem.GetOperations().Get(strings.ToLower(httpMethod))
	if !ok {
		return nil, nil, fmt.Errorf("operation not found: %s", httpMethod)
	}
	parameters = stringset.NewStringSet()
	query = stringset.NewStringSet()
	for _, param := range getDoc.Parameters {
		if param.In == "path" {
			parameters.Add(param.Name)
		}
		if param.In == "query" {
			query.Add(param.Name)
		}
	}
	return parameters, query, nil
}

// BuildClient is a function that builds partial client from a swagger file.
func BuildClient(ctx context.Context, kubeclient dynamic.Interface, swaggerPath string) (*UnstructuredClient, error) {
	basePath := "/tmp/rest-dynamic-controller"
	err := os.MkdirAll(basePath, 0755)
	defer os.RemoveAll(basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	fgetter := &fgetter.Filegetter{
		Client:     &http.Client{},
		KubeClient: kubeclient,
	}

	err = fgetter.GetFile(ctx, filepath.Join(basePath, filepath.Base(swaggerPath)), swaggerPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	}

	contents, _ := os.ReadFile(filepath.Join(basePath, pathutil.Base(swaggerPath)))
	d, err := libopenapi.NewDocument(contents)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	doc, modelErrors := d.BuildV3Model()
	if len(modelErrors) > 0 {
		return nil, fmt.Errorf("failed to build model: %w", errors.Join(modelErrors...))
	}
	if doc == nil {
		return nil, fmt.Errorf("failed to build model")
	}

	// Resolve model references
	resolvingErrors := doc.Index.GetResolver().Resolve()
	errs := []error{}
	for i := range resolvingErrors {
		errs = append(errs, resolvingErrors[i].ErrorRef)
	}
	if len(resolvingErrors) > 0 {
		return nil, fmt.Errorf("failed to resolve model references: %w", errors.Join(errs...))
	}
	if len(doc.Model.Servers) == 0 {
		return nil, fmt.Errorf("no servers found in the document")
	}

	return &UnstructuredClient{
		Server:    doc.Model.Servers[0].URL,
		DocScheme: doc,
	}, nil
}
