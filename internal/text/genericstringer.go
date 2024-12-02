package text

import (
	"encoding/json"
	"fmt"
	"reflect"
)

func GenericToString(i interface{}) (string, error) {
	if reflect.TypeOf(i).Kind() == reflect.String {
		return i.(string), nil
	}
	if reflect.TypeOf(i).Kind() == reflect.Float32 || reflect.TypeOf(i).Kind() == reflect.Float64 {
		return fmt.Sprintf("%d", int(i.(float64))), nil
	}
	if reflect.TypeOf(i).Kind() == reflect.Int || reflect.TypeOf(i).Kind() == reflect.Int32 || reflect.TypeOf(i).Kind() == reflect.Int64 || reflect.TypeOf(i).Kind() == reflect.Uint || reflect.TypeOf(i).Kind() == reflect.Uint32 || reflect.TypeOf(i).Kind() == reflect.Uint64 {
		return fmt.Sprintf("%d", i), nil
	}
	if reflect.TypeOf(i).Kind() == reflect.Bool {
		return fmt.Sprintf("%t", i.(bool)), nil
	}
	b, err := json.Marshal(i)
	return string(b), err
}
