package restclient

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	pathutil "path"
	"path/filepath"
	"strconv"
	"strings"

	stringset "github.com/krateoplatformops/rest-dynamic-controller/internal/text"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/comparison"
	fgetter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/filegetter"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	orderedmap "github.com/pb33f/libopenapi/orderedmap"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// TODO: to be re-enabled when libopenapi-validator is stable
// Validator defines the interface for request validation.
//type Validator interface {
//	ValidateRequest(req *http.Request) (bool, []error)
//}

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
	ValidateRequest(httpMethod string, path string, parameters map[string]string, query map[string]string, headers map[string]string, cookies map[string]string) error
	RequestedBody(httpMethod string, path string) (bodys stringset.StringSet, err error)
	RequestedParams(httpMethod string, path string) (parameters, query, headers, cookies stringset.StringSet, err error)
	FindBy(ctx context.Context, cli *http.Client, path string, conf *RequestConfiguration) (Response, error)
	Call(ctx context.Context, cli *http.Client, path string, conf *RequestConfiguration) (Response, error)
	//Validate(req *http.Request) (bool, []error) // TODO: to be re-enabled when libopenapi-validator is stable (to be renamed to ValidateRequest)
}

type UnstructuredClient struct {
	IdentifierFields       []string
	IdentifiersMatchPolicy string
	Resource               *unstructured.Unstructured
	//Doc                   libopenapi.Document         // Parsed OpenAPI document by libopenapi, needed for http request validation. TODO: to be re-enabled when libopenapi-validator is stable
	DocScheme *libopenapi.DocumentModel[v3.Document] // OpenAPI document model (high-level)
	Server    string
	Debug     bool
	SetAuth   func(req *http.Request)
	//Validator             Validator 				    // Validator for request validation. TODO: to be re-enabled when libopenapi-validator is stable
}

type RequestConfiguration struct {
	Parameters map[string]string // Path parameters
	Query      map[string]string
	Headers    map[string]string
	Cookies    map[string]string
	Body       any
	Method     string
}

// isInResource is a method used during a "FindBy" operation.
// It compares a value from an API response with the corresponding value in the local Unstructured resource.
// It checks for the identifier's presence and correctness in 'spec' first, then falls back to checking 'status'.
// TODO: to be evaluated for potential addition of `ResponseFieldMapping`
func (u *UnstructuredClient) isInResource(responseValue interface{}, fieldPath ...string) (bool, error) {
	if u.Resource == nil {
		return false, fmt.Errorf("resource is nil")
	}

	// Check 1: Look for the identifier in the 'spec'.
	if localValue, found, err := unstructured.NestedFieldNoCopy(u.Resource.Object, append([]string{"spec"}, fieldPath...)...); err == nil && found {
		// If the field is found in the spec, we compare it.
		// If it matches, we have a definitive match and can return true.
		log.Printf("isInResource - found in spec: localValue=%v, responseValue=%v", localValue, responseValue)
		//if comparison.DeepEqual(localValue, responseValue) {
		if comparison.CompareAny(localValue, responseValue) {
			log.Print("END isInResource - comparison CompareAny returned true ##########################")
			return true, nil
		} else {
			log.Print("isInResource - comparison CompareAny returned false")
		}
	} else if err != nil {
		return false, fmt.Errorf("error searching for identifier in spec: %w", err)
	}

	// Check 2: If the identifier was not found in spec, or if it was found but did not match,
	// we proceed to check the 'status'. This is common for server-assigned identifiers.
	// Last resort check, even if it makes less sense to search for findby identifiers in status.
	if localValue, found, err := unstructured.NestedFieldNoCopy(u.Resource.Object, append([]string{"status"}, fieldPath...)...); err == nil && found {
		// If found in status, we compare it. This is the last chance for a match.
		if comparison.CompareAny(localValue, responseValue) {
			return true, nil
		}
	} else if err != nil {
		return false, fmt.Errorf("error searching for identifier in status: %w", err)
	}

	log.Printf("isInResource - identifier not found in spec or status for path %v", fieldPath)
	// No match.
	return false, nil
}

// ValidateRequest is a method that validates the request parameters, query, headers, and cookies against the OpenAPI document.
// It checks if the required parameters are present and returns an error if any required parameter is missing.
func (u *UnstructuredClient) ValidateRequest(httpMethod string, path string, parameters map[string]string, query map[string]string, headers map[string]string, cookies map[string]string) error {
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
			if param.In == "header" {
				if _, ok := headers[param.Name]; !ok && !isAuthorizationHeader(param.Name) {
					return fmt.Errorf("missing header: %s", param.Name)
				}
			}
			if param.In == "cookie" {
				if _, ok := cookies[param.Name]; !ok {
					return fmt.Errorf("missing cookie: %s", param.Name)
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
func (u *UnstructuredClient) RequestedParams(httpMethod string, path string) (parameters, query, headers, cookies stringset.StringSet, err error) {
	pathItem, ok := u.DocScheme.Model.Paths.PathItems.Get(path)
	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("path not found: %s", path)
	}
	getDoc, ok := pathItem.GetOperations().Get(strings.ToLower(httpMethod))
	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("operation not found: %s", httpMethod)
	}
	parameters = stringset.NewStringSet()
	query = stringset.NewStringSet()
	headers = stringset.NewStringSet()
	cookies = stringset.NewStringSet()
	for _, param := range getDoc.Parameters {
		switch param.In {
		case "path":
			parameters.Add(param.Name)
		case "query":
			query.Add(param.Name)
		case "header":
			headers.Add(param.Name)
		case "cookie":
			cookies.Add(param.Name)
		default:
			return nil, nil, nil, nil, fmt.Errorf("unknown parameter location: %s", param.In)
		}
	}
	//fmt.Printf("RequestedParams: parameters=%v, query=%v, headers=%v, cookies=%v\n", parameters, query, headers, cookies)
	return
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

	// TODO: to be re-enabled when libopenapi-validator is stable
	//validator, err := NewOpenAPIValidator(d)
	//if err != nil {
	//	return nil, fmt.Errorf("failed to create validator: %w", err)
	//}

	return &UnstructuredClient{
		Server: doc.Model.Servers[0].URL,
		//Doc:       d,
		DocScheme: doc,
		//Validator: validator,
	}, nil
}

// isAuthorizationHeader checks if the given header is an authorization header or contains "authorization" (case-insensitive).
func isAuthorizationHeader(header string) bool {
	return strings.Contains(strings.ToLower(header), "authorization")
}
