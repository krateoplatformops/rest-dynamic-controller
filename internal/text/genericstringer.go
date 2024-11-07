package text

import (
	"fmt"
	"reflect"
)

func GenericToString(i interface{}) string {
	if reflect.TypeOf(i).Kind() == reflect.String {
		return i.(string)
	}
	if reflect.TypeOf(i).Kind() == reflect.Float32 || reflect.TypeOf(i).Kind() == reflect.Float64 {
		return fmt.Sprintf("%d", int(i.(float64)))
	}
	if reflect.TypeOf(i).Kind() == reflect.Int || reflect.TypeOf(i).Kind() == reflect.Int32 || reflect.TypeOf(i).Kind() == reflect.Int64 || reflect.TypeOf(i).Kind() == reflect.Uint || reflect.TypeOf(i).Kind() == reflect.Uint32 || reflect.TypeOf(i).Kind() == reflect.Uint64 {
		return fmt.Sprintf("%d", i)
	}
	if reflect.TypeOf(i).Kind() == reflect.Bool {
		return fmt.Sprintf("%t", i.(bool))
	}
	return ""
}
