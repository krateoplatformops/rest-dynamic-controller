package builder

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	unstructuredtools "github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured"

	restclient "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/client"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/deepcopy"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/pathparsing"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/krateoplatformops/rest-dynamic-controller/internal/text"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/client/apiaction"
	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"
)

type RequestedParams struct {
	Parameters text.StringSet // `Parameters` are the path parameters
	Query      text.StringSet
	Headers    text.StringSet
	Cookies    text.StringSet
	Body       text.StringSet
}

type CallInfo struct {
	Path                string
	ReqParams           *RequestedParams
	IdentifierFields    []string
	RequestFieldMapping []getter.RequestFieldMappingItem // RequestFieldMapping is specific for the call (action)
	Method              string
	Action              apiaction.APIAction
}

type APIFuncDef func(ctx context.Context, cli *http.Client, path string, conf *restclient.RequestConfiguration) (restclient.Response, error)

// APICallBuilder builds the API call based on the action and the info from the RestDefinition
func APICallBuilder(cli restclient.UnstructuredClientInterface, info *getter.Info, action apiaction.APIAction) (apifunc APIFuncDef, callInfo *CallInfo, err error) {
	identifierFields := info.Resource.Identifiers
	for _, descr := range info.Resource.VerbsDescription {
		if strings.EqualFold(descr.Action, action.String()) {
			params, query, headers, cookies, err := cli.RequestedParams(descr.Method, descr.Path)
			if err != nil {
				return nil, nil, fmt.Errorf("retrieving requested params: %s", err)
			}
			var body text.StringSet
			if descr.Method == "POST" || descr.Method == "PUT" || descr.Method == "PATCH" {
				body, err = cli.RequestedBody(descr.Method, descr.Path)
				if err != nil {
					return nil, nil, fmt.Errorf("retrieving requested body params: %s", err)
				}
				if body == nil {
					body = text.StringSet{}
				}
			}

			callInfo := &CallInfo{
				Path:   descr.Path,
				Method: descr.Method,
				Action: action,
				ReqParams: &RequestedParams{
					Parameters: params, // Path parameters
					Query:      query,
					Headers:    headers,
					Cookies:    cookies,
					Body:       body,
				},
				IdentifierFields:    identifierFields,
				RequestFieldMapping: descr.RequestFieldMapping,
			}

			switch action {
			case apiaction.FindBy: // FindBy is used to find the resource by the identifier fields (usually in a list of resources)
				return cli.FindBy, callInfo, nil // FindBy has its own function
			default:
				return cli.Call, callInfo, nil // Generic Call function
			}
		}
	}
	return nil, nil, nil
}

// BuildCallConfig builds the request configuration based on the callInfo
// and the fields from the spec and status of the main resource, the spec of the Configuration CR and also the request field mappings.
func BuildCallConfig(callInfo *CallInfo, mg *unstructured.Unstructured, configSpec map[string]interface{}) *restclient.RequestConfiguration {
	if callInfo == nil || mg == nil {
		return nil
	}

	reqConfiguration := &restclient.RequestConfiguration{}
	reqConfiguration.Parameters = make(map[string]string) // Path parameters
	reqConfiguration.Query = make(map[string]string)
	reqConfiguration.Headers = make(map[string]string)
	reqConfiguration.Cookies = make(map[string]string)
	reqConfiguration.Method = callInfo.Method
	mapBody := make(map[string]interface{})

	// 1. Apply fields from the Configuration CR.
	applyConfigSpec(reqConfiguration, configSpec, callInfo.Action.String())

	// 2. Apply explicit request field mappings.
	applyRequestFieldMapping(callInfo, mg, reqConfiguration, mapBody)

	specFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "spec")
	if err != nil {
		specFields = make(map[string]interface{}) // Initialize as empty map if error when retrieving spec
	}

	log.Printf("Spec fields retrieved from unstructured:\n")
	for k, v := range specFields {
		log.Printf("Spec field key: %s, value: %v\n", k, v)
	}

	statusFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "status")
	if err != nil {
		statusFields = make(map[string]interface{}) // Initialize as empty map if error when retrieving status
	}

	// 3. Apply values from the main resource's spec (spec takes precedence over status in case of duplicates).
	processFields(callInfo, specFields, reqConfiguration, mapBody)

	// 4. Apply values from the main resource's status
	processFields(callInfo, statusFields, reqConfiguration, mapBody)

	reqConfiguration.Body = mapBody

	log.Printf("[BuildCallConfig] reqConfiguration: %v\n", reqConfiguration)

	return reqConfiguration
}

// applyRequestFieldMapping populates the request configuration from the request field mappings.
func applyRequestFieldMapping(callInfo *CallInfo, mg *unstructured.Unstructured, reqConfiguration *restclient.RequestConfiguration, mapBody map[string]interface{}) {

	if callInfo.RequestFieldMapping == nil {
		return
	}

	for _, mapping := range callInfo.RequestFieldMapping {
		pathSegments, err := pathparsing.ParsePath(mapping.InCustomResource)
		if len(pathSegments) == 0 {
			continue
		}

		log.Printf("Path segments for InCustomResource %s: %v\n", mapping.InCustomResource, pathSegments)

		val, found, err := unstructured.NestedFieldNoCopy(mg.Object, pathSegments...)
		if err != nil || !found {
			continue
		}

		log.Printf("Value for InCustomResource %s: %v\n", mapping.InCustomResource, val)

		if mapping.InPath != "" {
			// parse InPath with pathparsing to be consistent with dot notation handling
			inPathSegments, err := pathparsing.ParsePath(mapping.InPath)
			if err != nil || len(inPathSegments) == 0 {
				log.Printf("Error parsing InPath %s: %s\n", mapping.InPath, err)
				continue
			}

			// it should be a single segment for path parameters since path parameters are flat
			if len(inPathSegments) != 1 {
				log.Printf("InPath %s has multiple segments after parsing, expected a single segment for path parameters\n", mapping.InPath)
				continue
			}

			mapping.InPath = inPathSegments[0]
			strVal := fmt.Sprintf("%v", val)
			reqConfiguration.Parameters[mapping.InPath] = strVal

		} else if mapping.InQuery != "" {
			// parse InQuery with pathparsing to be consistent with dot notation handling
			inQuerySegments, err := pathparsing.ParsePath(mapping.InQuery)
			if err != nil || len(inQuerySegments) == 0 {
				log.Printf("Error parsing InQuery %s: %s\n", mapping.InQuery, err)
				continue
			}

			// it should be a single segment for query parameters since query parameters are flat
			if len(inQuerySegments) != 1 {
				log.Printf("InQuery %s has multiple segments after parsing, expected a single segment for query parameters\n", mapping.InQuery)
				continue
			}

			mapping.InQuery = inQuerySegments[0]
			strVal := fmt.Sprintf("%v", val)
			reqConfiguration.Query[mapping.InQuery] = strVal

		} else if mapping.InBody != "" {

			log.Printf("Processing InBody mapping")

			// parse InBody with pathparsing to be consistent with dot notation handling
			inBodySegments, err := pathparsing.ParsePath(mapping.InBody)
			if err != nil || len(inBodySegments) == 0 {
				log.Printf("Error parsing InBody %s: %s\n", mapping.InBody, err)
				continue
			}

			log.Printf("InBody segments: %v\n", inBodySegments)

			// Perform deep copy and type conversions (e.g., float64 to int64).
			// This is needed since we will set the value in the body map and therefore we need to ensure the types are correct.
			// On the other hand, for path and query parameters we convert everything to string.
			convertedValue := deepcopy.DeepCopyJSONValue(val)

			// print map body before setting the value
			for k, v := range mapBody {
				log.Printf("Before setting, mapBody key: %s, value: %v\n", k, v)
			}

			// Set the value in the body map at the correct nested path
			err = unstructured.SetNestedField(mapBody, convertedValue, inBodySegments...)
			if err != nil {
				log.Printf("Error setting body field %s to value %v: %s\n", mapping.InBody, convertedValue, err)
				continue
			}

			// print map body
			for k, v := range mapBody {
				log.Printf("mapBody key: %s, value: %v\n", k, v)
			}
		}
	}
}

// applyConfigSpec populates the request configuration from a configuration spec map (coming from the Configuration CR)
func applyConfigSpec(req *restclient.RequestConfiguration, configSpec map[string]interface{}, action string) {
	if configSpec == nil {
		return
	}

	// Internal helper to avoid repetition
	process := func(key string, dest map[string]string) {
		if actionConfig, found, err := unstructured.NestedMap(configSpec, key, action); err == nil && found && actionConfig != nil {
			for k, v := range actionConfig {
				stringVal := fmt.Sprintf("%v", v) // Convert any type to string
				dest[k] = stringVal
			}
		}
	}

	process("path", req.Parameters)
	process("query", req.Query)
	process("headers", req.Headers)
	process("cookies", req.Cookies)
}

// IsResourceKnown tries to build the `get` action API Call, with the given specFields and statusFields values.
// If it is able to build the `get` action request, returns true, false otherwise.
// Usually the `get` action is used to retrieve the resource by its unique identifier (usually server-side generated and assigned).
// Therefore "known" in this case means that the resource can be retrieved by this kind of identifier.
// This function is used during the reconciliation to decide:
// - if the resource can be retrieved by its unique identifier (usually server-side generated and assigned) (e.g GET /resource/{id})
// - or if it needs to be found by its identifiers fields (e.g., unique name within a organization) in a list of resources (e.g GET /resources)
func IsResourceKnown(cli restclient.UnstructuredClientInterface, clientInfo *getter.Info, mg *unstructured.Unstructured) bool {
	if mg == nil || clientInfo == nil {
		return false
	}

	apiCall, callInfo, err := APICallBuilder(cli, clientInfo, apiaction.Get)
	if apiCall == nil || err != nil {
		return false
	}

	reqConfiguration := BuildCallConfig(callInfo, mg, clientInfo.ConfigurationSpec)
	if reqConfiguration == nil {
		return false
	}

	return cli.ValidateRequest(callInfo.Method, callInfo.Path, reqConfiguration.Parameters, reqConfiguration.Query, reqConfiguration.Headers, reqConfiguration.Cookies) == nil
}

func processFields(callInfo *CallInfo, fields map[string]interface{}, reqConfiguration *restclient.RequestConfiguration, mapBody map[string]interface{}) {
	log.Print("Inside processFields")
	for field, value := range fields {
		if field == "" {
			continue
		}

		if callInfo.ReqParams.Parameters.Contains(field) {
			if _, ok := reqConfiguration.Parameters[field]; !ok { // Avoid overwriting existing values
				reqConfiguration.Parameters[field] = fmt.Sprintf("%v", value)
			}
		}

		if callInfo.ReqParams.Query.Contains(field) {
			if _, ok := reqConfiguration.Query[field]; !ok { // Avoid overwriting existing values
				reqConfiguration.Query[field] = fmt.Sprintf("%v", value)
			}
		}

		// Note: probably headers and cookies are better to be set ONLY in the Configuration CR spec
		// (and currently it is only possible there)
		// Therefore, we do not set them here since we are processing only the main resource fields (spec/status) with this function.

		if callInfo.ReqParams.Body.Contains(field) {
			if mapBody[field] == nil {
				mapBody[field] = value
				log.Printf("Setting body field %s to value %v\n", field, value)
			}
		}
	}
}
