package restResources

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	restclient "github.com/krateoplatformops/rest-dynamic-controller/internal/client"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/text"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/apiaction"
	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/restclient"
	"github.com/krateoplatformops/unstructured-runtime/pkg/logging"
	unstructuredtools "github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured"
	"github.com/rs/zerolog/log"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

type APIFuncDef func(ctx context.Context, cli *http.Client, path string, conf *restclient.RequestConfiguration) (any, error)

// APICallBuilder builds the API call based on the action and the info from the RestDefinition
func APICallBuilder(cli *restclient.UnstructuredClient, info *getter.Info, action apiaction.APIAction) (apifunc APIFuncDef, callInfo *CallInfo, err error) {
	identifierFields := info.Resource.Identifiers
	for _, descr := range info.Resource.VerbsDescription {
		if strings.EqualFold(descr.Action, action.String()) {
			params, query, err := cli.RequestedParams(descr.Method, descr.Path)
			if err != nil {
				return nil, nil, fmt.Errorf("error retrieving requested params: %s", err)
			}
			var body text.StringSet
			if descr.Method == "POST" || descr.Method == "PUT" || descr.Method == "PATCH" {
				body, err = cli.RequestedBody(descr.Method, descr.Path)
				if err != nil {
					return nil, nil, fmt.Errorf("error retrieving requested body params: %s", err)
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

// isCRUpdated checks if the CR was updated by comparing the fields in the CR with the response from the API call, if existing cr fields are different from the response, it returns false
func isCRUpdated(mg *unstructured.Unstructured, rm map[string]interface{}) (ComparisonResult, error) {
	m, err := unstructuredtools.GetFieldsFromUnstructured(mg, "spec")
	if err != nil {
		return ComparisonResult{
			IsEqual: false,
			Reason: &Reason{
				Reason: "error getting spec fields",
			},
		}, fmt.Errorf("error getting spec fields: %w", err)
	}

	return compareExisting(m, rm)
}

type Reason struct {
	Reason      string
	FirstValue  any
	SecondValue any
}

type ComparisonResult struct {
	IsEqual bool
	Reason  *Reason
}

func (r ComparisonResult) String() string {
	if r.IsEqual {
		return "ComparisonResult: IsEqual=true"
	}
	if r.Reason == nil {
		return "ComparisonResult: IsEqual=false, Reason=nil"
	}
	return fmt.Sprintf("ComparisonResult: IsEqual=false, Reason=%s, FirstValue=%v, SecondValue=%v", r.Reason.Reason, r.Reason.FirstValue, r.Reason.SecondValue)
}

// compareExisting recursively compares fields between two maps and logs differences.
func compareExisting(mg map[string]interface{}, rm map[string]interface{}, path ...string) (ComparisonResult, error) {
	for key, value := range mg {
		currentPath := append(path, key)
		pathStr := fmt.Sprintf("%v", currentPath)

		rmValue, ok := rm[key]
		if !ok {
			continue
		}

		// fmt.Println("Comparing", pathStr, value, rmValue)

		if reflect.TypeOf(value).Kind() != reflect.TypeOf(rmValue).Kind() {
			return ComparisonResult{
				IsEqual: false,
				Reason: &Reason{
					Reason:      "types differ",
					FirstValue:  value,
					SecondValue: rmValue,
				},
			}, fmt.Errorf("types differ at %s - %s is different from %s", pathStr, reflect.TypeOf(value).Kind(), reflect.TypeOf(rmValue).Kind())
		}

		switch reflect.TypeOf(value).Kind() {
		case reflect.Map:
			mgMap, ok1 := value.(map[string]interface{})
			if !ok1 {
				// fmt.Printf("Type assertion failed for map at '%s'\n", pathStr)
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "type assertion failed",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, fmt.Errorf("type assertion failed for map at %s", pathStr)
			}
			rmMap, ok2 := rmValue.(map[string]interface{})
			if !ok2 {
				// fmt.Printf("Type assertion failed for map at '%s'\n", pathStr)
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "type assertion failed",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, fmt.Errorf("type assertion failed for map at %s", pathStr)
			}
			res, err := compareExisting(mgMap, rmMap, currentPath...)
			if err != nil {
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "error comparing maps",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, err
			}
			if !res.IsEqual {
				// fmt.Printf("Values differ at '%s'\n", pathStr)
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "values differ",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, nil
			}
		case reflect.Slice:
			valueSlice, ok1 := value.([]interface{})
			if !ok1 || reflect.TypeOf(rmValue).Kind() != reflect.Slice {
				// fmt.Printf("Values are not both slices or type assertion failed at '%s'\n", pathStr)
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "values are not both slices or type assertion failed",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, fmt.Errorf("values are not both slices or type assertion failed at %s", pathStr)
			}
			rmSlice, ok2 := rmValue.([]interface{})
			if !ok2 {
				// fmt.Printf("Type assertion failed for slice at '%s'\n", pathStr)
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "values are not both slices or type assertion failed",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, fmt.Errorf("type assertion failed for slice at %s", pathStr)
			}
			for i, v := range valueSlice {
				if reflect.TypeOf(v).Kind() == reflect.Map {
					mgMap, ok1 := v.(map[string]interface{})
					if !ok1 {
						// fmt.Printf("Type assertion failed for map at '%s'\n", pathStr)
						return ComparisonResult{
							IsEqual: false,
							Reason: &Reason{
								Reason:      "type assertion failed",
								FirstValue:  value,
								SecondValue: rmValue,
							},
						}, fmt.Errorf("type assertion failed for map at %s", pathStr)
					}
					rmMap, ok2 := rmSlice[i].(map[string]interface{})
					if !ok2 {
						// fmt.Printf("Type assertion failed for map at '%s'\n", pathStr)
						return ComparisonResult{
							IsEqual: false,
							Reason: &Reason{
								Reason:      "type assertion failed",
								FirstValue:  value,
								SecondValue: rmValue,
							},
						}, fmt.Errorf("type assertion failed for map at %s", pathStr)
					}
					res, err := compareExisting(mgMap, rmMap, currentPath...)
					if err != nil {
						return ComparisonResult{
							IsEqual: false,
							Reason: &Reason{
								Reason:      "error comparing maps",
								FirstValue:  value,
								SecondValue: rmValue,
							},
						}, err
					}
					if !res.IsEqual {
						// fmt.Printf("Values differ at '%s'\n", pathStr)
						return ComparisonResult{
							IsEqual: false,
							Reason: &Reason{
								Reason:      "values differ",
								FirstValue:  value,
								SecondValue: rmValue,
							},
						}, nil
					}
				} else if v != rmSlice[i] {
					// fmt.Printf("Values differ at '%s'\n", pathStr)
					return ComparisonResult{
						IsEqual: false,
						Reason: &Reason{
							Reason:      "values differ",
							FirstValue:  value,
							SecondValue: rmValue,
						},
					}, nil
				}
			}
		default:
			ok, err := compareAny(value, rmValue)
			if err != nil {
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "error comparing values",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, err
			}
			if !ok {
				// fmt.Printf("Values differ at '%s' %s %s\n", pathStr, value, rmValue)
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "values differ",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, nil
			}
		}
	}

	return ComparisonResult{IsEqual: true}, nil
}
func numberCaster(value interface{}) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v // No conversion needed since v is already int64
	case uint:
		return int64(v)
	case uint8:
		return int64(v)
	case uint16:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		return int64(v)
	case float32:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return -999999 // Return a default value if none of the cases match
	}
}

func compareAny(a any, b any) (bool, error) {
	//if is number compare as number
	switch a.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		ia := numberCaster(a)
		ib := numberCaster(b)
		return ia == ib, nil
	case string:
		sa, ok := a.(string)
		if !ok {
			return false, fmt.Errorf("type assertion failed - to string: %v", a)
		}
		sb, ok := b.(string)
		if !ok {
			return false, fmt.Errorf("type assertion failed - to string: %v", b)
		}
		return sa == sb, nil
	case bool:
		ba, ok := a.(bool)
		if !ok {
			return false, fmt.Errorf("type assertion failed - to bool: %v", a)
		}
		bb, ok := b.(bool)
		if !ok {
			return false, fmt.Errorf("type assertion failed - to bool: %v", b)
		}
		return ba == bb, nil
	default:
		return reflect.DeepEqual(a, b), nil
	}
}

// populateStatusFields populates the status fields in the mg object with the values from the body
func populateStatusFields(clientInfo *getter.Info, mg *unstructured.Unstructured, body map[string]interface{}) error {
	if body != nil {
		for k, v := range body {
			for _, identifier := range clientInfo.Resource.Identifiers {
				if k == identifier {
					stringValue, err := text.GenericToString(v)
					if err != nil {
						log.Err(err).Msg("Converting value to string")
						return err
					}
					err = unstructured.SetNestedField(mg.Object, stringValue, "status", identifier)
					if err != nil {
						log.Err(err).Msg("Setting identifier")
						return err
					}
				}
			}
		}
	}
	return nil
}

// tries to find the resource in the cluster, with the given statusFields and specFields values, if it is able to validate the GET request, returns true
func isResourceKnown(cli *restclient.UnstructuredClient, log logging.Logger, clientInfo *getter.Info, statusFields map[string]interface{}, specFields map[string]interface{}) bool {
	apiCall, callInfo, err := APICallBuilder(cli, clientInfo, apiaction.Get)
	if apiCall == nil {
		return false
	}
	if err != nil {
		log.Debug("Building API call", "error", err)
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
