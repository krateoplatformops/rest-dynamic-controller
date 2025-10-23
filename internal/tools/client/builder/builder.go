package builder

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	unstructuredtools "github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured"

	restclient "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/client"
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
		specFields = make(map[string]interface{}) // Initialize as empty map
	}
	statusFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "status")
	if err != nil {
		statusFields = make(map[string]interface{}) // Initialize as empty map
	}

	// 3. Apply values from the main resource's spec (spec takes precedence over status in case of duplicates).
	processFields(callInfo, specFields, reqConfiguration, mapBody)

	// 4. Apply values from the main resource's status
	processFields(callInfo, statusFields, reqConfiguration, mapBody)

	reqConfiguration.Body = mapBody

	return reqConfiguration
}

// applyRequestFieldMapping populates the request configuration from the request field mappings.
func applyRequestFieldMapping(callInfo *CallInfo, mg *unstructured.Unstructured, reqConfiguration *restclient.RequestConfiguration, mapBody map[string]interface{}) {

	if callInfo.RequestFieldMapping == nil {
		//log.Printf("No RequestFieldMapping defined for action %s\n", callInfo.Action.String())
		return
	}

	for _, mapping := range callInfo.RequestFieldMapping {
		pathParts := strings.Split(mapping.InCustomResource, ".")
		if len(pathParts) == 0 {
			continue
		}

		val, found, err := unstructured.NestedFieldNoCopy(mg.Object, pathParts...)
		if err != nil {
			//log.Printf("Error retrieving field %s from custom resource to be used in RequestFieldMapping: %s\n", mapping.InCustomResource, err)
			continue
		}
		if !found {
			//log.Printf("Field %s not found in custom resource to be used in RequestFieldMapping\n", mapping.InCustomResource)
			continue
		}

		//log.Printf("Processing RequestFieldMapping: custom resource field %s with value %v\n", mapping.InCustomResource, val)

		if mapping.InPath != "" {
			//log.Printf("Mapping to path parameter %s\n", mapping.InPath)
			strVal := fmt.Sprintf("%v", val)
			reqConfiguration.Parameters[mapping.InPath] = strVal
			//log.Printf("Added mapping field %s to path parameter %s with value %s\n", mapping.InCustomResource, mapping.InPath, strVal)
		} else if mapping.InQuery != "" {
			//log.Printf("Mapping to query parameter %s\n", mapping.InQuery)
			strVal := fmt.Sprintf("%v", val)
			reqConfiguration.Query[mapping.InQuery] = strVal
			//log.Printf("Added mapping field %s to query parameter %s with value %s\n", mapping.InCustomResource, mapping.InQuery, strVal)
		} else if mapping.InBody != "" {
			//log.Printf("Mapping to body field %s\n", mapping.InBody)
			mapBody[mapping.InBody] = val
			//log.Printf("Added mapping field %s to body field %s with value %v\n", mapping.InCustomResource, mapping.InBody, val)
		}
	}
}

// applyConfigSpec populates the request configuration from a configuration spec map (coming from the Configuration CR)
func applyConfigSpec(req *restclient.RequestConfiguration, configSpec map[string]interface{}, action string) {
	if configSpec == nil {
		return
	}

	//fmt.Printf("Applying config spec for action: %s\n", action)
	//fmt.Printf("Config spec content: %v\n", configSpec)

	// Internal helper to avoid repetition
	process := func(key string, dest map[string]string) {
		if actionConfig, found, err := unstructured.NestedMap(configSpec, key, action); err == nil && found && actionConfig != nil {
			for k, v := range actionConfig {
				stringVal := fmt.Sprintf("%v", v) // Convert any type to string
				dest[k] = stringVal
				//fmt.Printf("%s param set from config spec: %s=%s\n", key, k, stringVal)
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
	if apiCall == nil {
		return false
	}
	if err != nil {
		return false
	}
	reqConfiguration := BuildCallConfig(callInfo, mg, clientInfo.ConfigurationSpec)
	if reqConfiguration == nil {
		return false
	}

	actionGetMethod := "GET"
	for _, descr := range clientInfo.Resource.VerbsDescription {
		if strings.EqualFold(descr.Action, apiaction.Get.String()) { // Needed if the `get` action in RestDefinition is not mapped to GET method (probably very uncommon)
			actionGetMethod = descr.Method
		}
	}

	return cli.ValidateRequest(actionGetMethod, callInfo.Path, reqConfiguration.Parameters, reqConfiguration.Query, reqConfiguration.Headers, reqConfiguration.Cookies) == nil
}

func processFields(callInfo *CallInfo, fields map[string]interface{}, reqConfiguration *restclient.RequestConfiguration, mapBody map[string]interface{}) {
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
		// Therefore, we do not set them here since we are processing the main resource fields

		if callInfo.ReqParams.Body.Contains(field) {
			if mapBody[field] == nil {
				mapBody[field] = value
			}
		}
	}
}
