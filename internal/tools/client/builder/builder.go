package builder

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	restclient "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/client"

	"github.com/krateoplatformops/rest-dynamic-controller/internal/text"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/client/apiaction"
	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"
)

type RequestedParams struct {
	Parameters text.StringSet
	Query      text.StringSet
	Body       text.StringSet
}

type CallInfo struct {
	Path             string
	ReqParams        *RequestedParams
	IdentifierFields []string
	Method           string
}

type APIFuncDef func(ctx context.Context, cli *http.Client, path string, conf *restclient.RequestConfiguration) (restclient.Response, error)

// APICallBuilder builds the API call based on the action and the info from the RestDefinition
func APICallBuilder(cli restclient.UnstructuredClientInterface, info *getter.Info, action apiaction.APIAction) (apifunc APIFuncDef, callInfo *CallInfo, err error) {
	identifierFields := info.Resource.Identifiers
	for _, descr := range info.Resource.VerbsDescription {
		if strings.EqualFold(descr.Action, action.String()) {
			params, query, err := cli.RequestedParams(descr.Method, descr.Path)
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
				ReqParams: &RequestedParams{
					Parameters: params,
					Query:      query,
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
func BuildCallConfig(callInfo *CallInfo, statusFields map[string]interface{}, specFields map[string]interface{}) *restclient.RequestConfiguration {
	reqConfiguration := &restclient.RequestConfiguration{}
	reqConfiguration.Parameters = make(map[string]string)
	reqConfiguration.Query = make(map[string]string)
	reqConfiguration.Method = callInfo.Method
	mapBody := make(map[string]interface{})

	processFields(callInfo, specFields, reqConfiguration, mapBody)
	processFields(callInfo, statusFields, reqConfiguration, mapBody)
	reqConfiguration.Body = mapBody
	return reqConfiguration
}

// tries to build the GET API Call, with the given statusFields and specFields values, if it is able to validate the GET request, returns true
func IsResourceKnown(cli restclient.UnstructuredClientInterface, clientInfo *getter.Info, statusFields map[string]interface{}, specFields map[string]interface{}) bool {
	apiCall, callInfo, err := APICallBuilder(cli, clientInfo, apiaction.Get)
	if apiCall == nil {
		return false
	}
	if err != nil {
		return false
	}
	reqConfiguration := BuildCallConfig(callInfo, statusFields, specFields)
	if reqConfiguration == nil {
		return false
	}

	actionGetMethod := "GET"
	for _, descr := range clientInfo.Resource.VerbsDescription {
		if strings.EqualFold(descr.Action, apiaction.Get.String()) {
			actionGetMethod = descr.Method
		}
	}

	return cli.ValidateRequest(actionGetMethod, callInfo.Path, reqConfiguration.Parameters, reqConfiguration.Query) == nil
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
		} else if callInfo.ReqParams.Body.Contains(field) {
			if mapBody[field] == nil {
				mapBody[field] = value
			}
		}
	}
}
