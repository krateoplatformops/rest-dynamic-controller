package restclient

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	stringset "github.com/krateoplatformops/rest-dynamic-controller/internal/text"
	fgetter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/filegetter"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	orderedmap "github.com/pb33f/libopenapi/orderedmap"
)

type APICallType string

const (
	APICallsTypeGet    APICallType = "get"
	APICallsTypePost   APICallType = "post"
	APICallsTypeList   APICallType = "list"
	APICallsTypeDelete APICallType = "delete"
	APICallsTypePatch  APICallType = "patch"
	APICallsTypeFindBy APICallType = "findby"
	APICallsTypePut    APICallType = "put"
)

func (a APICallType) String() string {
	return string(a)
}
func StringToApiCallType(ty string) (APICallType, error) {
	ty = strings.ToLower(ty)
	switch ty {
	case "get":
		return APICallsTypeGet, nil
	case "post":
		return APICallsTypePost, nil
	case "list":
		return APICallsTypeList, nil
	case "delete":
		return APICallsTypeDelete, nil
	case "patch":
		return APICallsTypePatch, nil
	case "findby":
		return APICallsTypeFindBy, nil
	case "put":
		return APICallsTypePut, nil
	}
	return "", fmt.Errorf("unknown api call type: %s", ty)
}

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

func (e *APIError) Error() string {
	return fmt.Sprintf("error: %s (%s, %d)", e.Message, e.TypeKey, e.EventID)
}

func buildPath(baseUrl string, path string, parameters map[string]string, query map[string]string) *url.URL {
	for key, param := range parameters {
		path = strings.Replace(path, fmt.Sprintf("{%s}", key), fmt.Sprintf("%v", param), 1)
	}

	params := url.Values{}

	for key, param := range query {
		params.Add(key, param)
	}

	parsed, err := url.Parse(baseUrl)
	if err != nil {
		return nil
	}
	parsed.Path = path
	parsed.RawQuery = params.Encode()
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

func (u *UnstructuredClient) RequestedBody(httpMethod string, path string) (bodyParams stringset.StringSet, err error) {
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

	// for _, proxy := range schema.AllOf {
	// 	propSchema, err := proxy.BuildSchema()
	// 	if err != nil {
	// 		return nil, fmt.Errorf("building schema for %s: %w", path, err)
	// 	}
	// 	// Iterate over the properties of the schema with First() and Next()
	// 	for prop := propSchema.Properties.First(); prop != nil; prop = prop.Next() {
	// 		// Add the property to the schema
	// 		bodyParams.Add(prop.Key())
	// 	}
	// }

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
func BuildClient(swaggerPath string) (*UnstructuredClient, error) {
	basePath := "/tmp/rest-dynamic-controller"
	err := os.MkdirAll(basePath, 0755)
	defer os.RemoveAll(basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}
	err = fgetter.GetFile(filepath.Join(basePath, filepath.Base(swaggerPath)), swaggerPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	}

	contents, _ := os.ReadFile(filepath.Join(basePath, path.Base(swaggerPath)))
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
		Auth:      nil,
	}, nil
}
