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
	Parameters text.StringSet
	Query      text.StringSet
	Headers    text.StringSet
	Cookies    text.StringSet
	Body       text.StringSet
}

type CallInfo struct {
	Path             string
	ReqParams        *RequestedParams
	IdentifierFields []string
	Method           string
	Action           apiaction.APIAction
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
					Parameters: params,
					Query:      query,
					Headers:    headers,
					Cookies:    cookies,
					Body:       body,
				},
				IdentifierFields: identifierFields,
			}

			switch action {
			// FindBy is used to find the resource by the identifier fields
			case apiaction.FindBy:
				return cli.FindBy, callInfo, nil
			default:
				return cli.Call, callInfo, nil
			}
		}
	}
	return nil, nil, nil
}

// BuildCallConfig builds the request configuration based on the callInfo and the fields from the status and spec
func BuildCallConfig(callInfo *CallInfo, mg *unstructured.Unstructured, configSpec map[string]interface{}) *restclient.RequestConfiguration {
	if callInfo == nil || mg == nil {
		return nil
	}

	reqConfiguration := &restclient.RequestConfiguration{}
	reqConfiguration.Parameters = make(map[string]string)
	reqConfiguration.Query = make(map[string]string)
	reqConfiguration.Headers = make(map[string]string)
	reqConfiguration.Cookies = make(map[string]string)
	reqConfiguration.Method = callInfo.Method
	mapBody := make(map[string]interface{})

	// Apply fields from the Configuration CR first.
	if configSpec != nil {
		action := callInfo.Action.String()

		if headers, _, _ := unstructured.NestedStringMap(configSpec, "headers", action); headers != nil {
			for k, v := range headers {
				reqConfiguration.Headers[k] = v
				fmt.Printf("[BUILDER - BuildCallConfig] Header %s: %s\n", k, v) // Debugging output
			}
		}

		if query, _, _ := unstructured.NestedStringMap(configSpec, "query", action); query != nil {
			for k, v := range query {
				reqConfiguration.Query[k] = v
				fmt.Printf("[BUILDER - BuildCallConfig] Query %s: %s\n", k, v) // Debugging output
			}
		}

		if cookies, _, _ := unstructured.NestedStringMap(configSpec, "cookies", action); cookies != nil {
			for k, v := range cookies {
				reqConfiguration.Cookies[k] = v
				fmt.Printf("[BUILDER - BuildCallConfig] Cookie %s: %s\n", k, v) // Debugging output
			}
		}

		if pathParams, _, _ := unstructured.NestedStringMap(configSpec, "path", action); pathParams != nil {
			for k, v := range pathParams {
				reqConfiguration.Parameters[k] = v
				fmt.Printf("[BUILDER - BuildCallConfig] Path Parameter %s: %s\n", k, v) // Debugging output
			}
		}
	}

	// Apply values from the main resource's spec and status
	statusFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "status")
	if err != nil {
		// If the status is not found, it means that the resource is not created yet
		// The error is not returned here, as it is not critical for the validation
		// log.Debug("Status not found")
		statusFields = make(map[string]interface{})
	}
	specFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "spec")
	if err != nil {
		// If the spec is not found, it means that the resource is not created yet
		// The error is not returned here, as it is not critical for the validation
		// log.Debug("Spec not found")
		specFields = make(map[string]interface{})
	}

	processFields(callInfo, specFields, reqConfiguration, mapBody)
	processFields(callInfo, statusFields, reqConfiguration, mapBody)

	reqConfiguration.Body = mapBody
	return reqConfiguration
}

// tries to build the GET API Call, with the given statusFields and specFields values, if it is able to validate the GET request, returns true
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
		if strings.EqualFold(descr.Action, apiaction.Get.String()) {
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
			stringVal := fmt.Sprintf("%v", value)
			if stringVal == "" && reqConfiguration.Parameters[field] != "" {
				continue
			}
			reqConfiguration.Parameters[field] = stringVal
		} else if callInfo.ReqParams.Query.Contains(field) {
			stringVal := fmt.Sprintf("%v", value)
			if stringVal == "" && reqConfiguration.Query[field] != "" {
				continue
			}
			reqConfiguration.Query[field] = stringVal
			// note: probably headers and cookies are better to be set ONLY in the configuration spec

			//} else if callInfo.ReqParams.Headers.Contains(field) {
			//	stringVal := fmt.Sprintf("%v", value)
			//	if stringVal == "" && reqConfiguration.Headers[field] != "" {
			//		continue
			//	}
			//	reqConfiguration.Headers[field] = stringVal
			//} else if callInfo.ReqParams.Cookies.Contains(field) {
			//	stringVal := fmt.Sprintf("%v", value)
			//	if stringVal == "" && reqConfiguration.Cookies[field] != "" {
			//		continue
			//	}
			//	reqConfiguration.Cookies[field] = stringVal
		} else if callInfo.ReqParams.Body.Contains(field) {
			if mapBody[field] == nil {
				mapBody[field] = value
			}
		}
	}
}
