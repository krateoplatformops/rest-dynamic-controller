package restResources

import (
	"fmt"
	"reflect"
	"testing"

	restclient "github.com/krateoplatformops/rest-dynamic-controller/internal/client"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/text"
)

func TestBuildCallConfig_AllFields(t *testing.T) {
	callInfo := &CallInfo{
		Method: "POST",
		ReqParams: &RequestedParams{
			Parameters: text.StringSet{"param1": {}, "param2": {}},
			Query:      text.StringSet{"query1": {}, "query2": {}},
			Body:       text.StringSet{"body1": {}, "body2": {}},
		},
	}

	statusFields := map[string]interface{}{
		"param1": "statusParam1",
		"query1": "statusQuery1",
		"body1":  "statusBody1",
	}
	specFields := map[string]interface{}{
		"param2": "spec%2FParam2",
		"query2": "specQuery2",
		"body2":  "specBody2",
	}

	got := BuildCallConfig(callInfo, statusFields, specFields)

	wantParams := map[string]string{
		"param1": "statusParam1",
		"param2": "spec%2FParam2",
	}
	wantQuery := map[string]string{
		"query1": "statusQuery1",
		"query2": "specQuery2",
	}
	wantBody := map[string]interface{}{
		"body1": "statusBody1",
		"body2": "specBody2",
	}

	if !reflect.DeepEqual(got.Parameters, wantParams) {
		t.Errorf("Parameters = %v, want %v", got.Parameters, wantParams)
	}
	if !reflect.DeepEqual(got.Query, wantQuery) {
		t.Errorf("Query = %v, want %v", got.Query, wantQuery)
	}
	if !reflect.DeepEqual(got.Body, wantBody) {
		t.Errorf("Body = %v, want %v", got.Body, wantBody)
	}
	if got.Method != "POST" {
		t.Errorf("Method = %v, want POST", got.Method)
	}
}

func TestBuildCallConfig_EmptyFields(t *testing.T) {
	callInfo := &CallInfo{
		Method: "GET",
		ReqParams: &RequestedParams{
			Parameters: text.StringSet{},
			Query:      text.StringSet{},
			Body:       text.StringSet{},
		},
	}
	statusFields := map[string]interface{}{}
	specFields := map[string]interface{}{}

	got := BuildCallConfig(callInfo, statusFields, specFields)

	if len(got.Parameters) != 0 {
		t.Errorf("Parameters should be empty, got %v", got.Parameters)
	}
	if len(got.Query) != 0 {
		t.Errorf("Query should be empty, got %v", got.Query)
	}
	bodyMap, ok := got.Body.(map[string]interface{})
	if !ok {
		t.Errorf("Body should be a map[string]interface{}, got %T", got.Body)
	} else if len(bodyMap) != 0 {
		t.Errorf("Body should be empty, got %v", got.Body)
	}
	if got.Method != "GET" {
		t.Errorf("Method = %v, want GET", got.Method)
	}
}

func TestProcessFields_PrioritizeNonEmpty(t *testing.T) {
	callInfo := &CallInfo{
		ReqParams: &RequestedParams{
			Parameters: text.StringSet{"foo": {}},
			Query:      text.StringSet{},
			Body:       text.StringSet{},
		},
	}
	reqConf := &restclient.RequestConfiguration{
		Parameters: map[string]string{"foo": "existing"},
		Query:      map[string]string{},
	}
	mapBody := map[string]interface{}{}

	fields := map[string]interface{}{
		"foo": "",
	}
	processFields(callInfo, fields, reqConf, mapBody)
	// Should not overwrite existing non-empty value with empty string
	if reqConf.Parameters["foo"] != "existing" {
		t.Errorf("Expected 'foo' to remain 'existing', got %q", reqConf.Parameters["foo"])
	}
}

func TestProcessFields_BodyField(t *testing.T) {
	callInfo := &CallInfo{
		ReqParams: &RequestedParams{
			Parameters: text.StringSet{},
			Query:      text.StringSet{},
			Body:       text.StringSet{"bar": {}},
		},
	}
	reqConf := &restclient.RequestConfiguration{
		Parameters: map[string]string{},
		Query:      map[string]string{},
	}
	mapBody := map[string]interface{}{}

	fields := map[string]interface{}{
		"bar": 123,
	}
	processFields(callInfo, fields, reqConf, mapBody)
	if v, ok := mapBody["bar"]; !ok || v != 123 {
		t.Errorf("Expected mapBody['bar'] = 123, got %v", mapBody["bar"])
	}
}

func TestProcessFields_QueryField(t *testing.T) {
	callInfo := &CallInfo{
		ReqParams: &RequestedParams{
			Parameters: text.StringSet{},
			Query:      text.StringSet{"baz": {}},
			Body:       text.StringSet{},
		},
	}
	reqConf := &restclient.RequestConfiguration{
		Parameters: map[string]string{},
		Query:      map[string]string{},
	}
	mapBody := map[string]interface{}{}

	fields := map[string]interface{}{
		"baz": 456,
	}
	processFields(callInfo, fields, reqConf, mapBody)
	if v, ok := reqConf.Query["baz"]; !ok || v != fmt.Sprintf("%v", 456) {
		t.Errorf("Expected Query['baz'] = '456', got %v", reqConf.Query["baz"])
	}
}
