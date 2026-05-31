// You MUST NOT modify this file.
//
// You MUST use JSONValue for all JSON values of unknown type (in other words
// the contents of documents).
//
// This package provides functionality to process and compare arbitrary JSON
// values.  The JSONValue type is a wrapper that holds unmarshaled JSON data.
// You should *only* access such JSON data using visitors with Accept, compare
// such JSON data using Equal, or validate such JSON data using Validate.
package jsondata

import (
	"fmt"
)

// A Visitor is used by Accept to process arbitrary values that only contain
// data of valid JSON types.  The Map and Slice methods may recursively call
// Accept on their constituent elements.
type Visitor[T any] interface {
	Map(map[string]JSONValue) (T, error)
	Slice([]JSONValue) (T, error)
	Bool(bool) (T, error)
	Float64(float64) (T, error)
	String(string) (T, error)
	Null() (T, error)
}

// Accept applies the given input visitor to the given input value by calling
// the appropriate visitor method on a copy of the input value given the input
// value's type. Returns an error if value is of a type that is not a valid JSON
// type or if the visitor method returns an error.
//
// As the visitor methods receive *copies* of the JSON values, any modifications
// will not affect the original JSON value.
//
// Note that this is a function so that the visitor and return value can be a
// generic type.  Go does not allow methods to have generic types unrelated to
// the receiver type.
func Accept[T any](value JSONValue, visitor Visitor[T]) (T, error) {
	wrap := func(v any) JSONValue {
		switch v := v.(type) {
		case JSONValue:
			return v
		default:
			return JSONValue{v}
		}
	}

	switch val := value.data.(type) {
	case map[string]any:
		m := make(map[string]JSONValue)
		for k, v := range val {
			m[k] = wrap(v)
		}
		return visitor.Map(m)
	case []any:
		s := make([]JSONValue, len(val))
		for i, v := range val {
			s[i] = wrap(v)
		}
		return visitor.Slice(s)
	case float64:
		return visitor.Float64(val)
	case bool:
		return visitor.Bool(val)
	case string:
		return visitor.String(val)
	case nil:
		return visitor.Null()
	default:
		var zero T
		return zero, fmt.Errorf("invalid JSON value: %s", value.data)
	}
}
