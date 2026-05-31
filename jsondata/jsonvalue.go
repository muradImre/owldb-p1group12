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
	"encoding/json"
	"fmt"
	"reflect"
)

// A Validator is used to validate an unmarshaled JSON value.  The Validate
// method should return an error if the given JSON value is invalid and nil
// otherwise.
type Validator interface {
	Validate(any) error
}

// JSONValue is a wrapper around any type that is a valid JSON type.  Encoded
// JSON data should be unmarshaled directly into a variable of type JSONValue.
// JSONValues should then *only* be accessed using visitors with Accept,
// compared to other JSONValues using Equal, or validated by passing a
// validation function to the the Validate method.
//
// The zero value is the JSON null value.
//
// You can create JSONValues three ways:
//
// 1. Declare a variable of type JSONValue, which will represent the JSON null
// value.
//
// var j JSONValue
//
// 2. Unmarshal encoded JSON data directly into a variable of type JSONValue.
//
// var j JSONValue
// err := json.Unmarshal(data, &j)
//
// 3. Use the NewJSONValue function to wrap a Go value of any valid JSON type.
// This function will return an error if the input value is not a valid JSON
// type.
//
// j, err := NewJSONValue(v)
//
// To convert a JSONValue (j) back to raw JSON data as a slide of bytes (b),
// use json.Marshal:
//
// b, err := json.Marshal(j)
type JSONValue struct {
	data any
}

func unwrap(v any) (any, error) {
	var err error

	// Handle nil first
	if v == nil {
		return nil, nil
	}

	// Handle JSONValue and primitive types with a type switch
	switch j := v.(type) {
	case JSONValue:
		// Already a valid JSONValue, just return its data.
		return j.data, nil
	case float64, bool, string:
		// These types are already valid JSON types.
		return v, nil
	}

	// Handle collection types recursively using reflection.
	// Should only be map[string]any or []any.
	val := reflect.ValueOf(v)
	switch val.Kind() {
	case reflect.Map:
		// Ensure that the map's keys are strings and unwrap its values.
		m := make(map[string]any)
		iter := val.MapRange()
		for iter.Next() {
			key := iter.Key()
			if key.Kind() != reflect.String {
				return nil, fmt.Errorf("invalid JSON key: %v", key)
			}
			k := key.String()
			value := iter.Value()
			m[k], err = unwrap(value.Interface())
			if err != nil {
				return nil, err
			}
		}
		return m, nil
	case reflect.Slice:
		// Unwrap the slice's elements.
		s := make([]any, val.Len())
		for i := 0; i < val.Len(); i++ {
			value := val.Index(i)
			s[i], err = unwrap(value.Interface())
			if err != nil {
				return nil, err
			}
		}
		return s, nil
	default:
		return nil, fmt.Errorf("invalid JSON value: %v", v)
	}
}

// NewJSONValue returns a new JSONValue that wraps the given input value.
func NewJSONValue(v any) (JSONValue, error) {
	// Unwrap the input value and wrap it in a new JSONValue.
	data, err := unwrap(v)
	return JSONValue{data}, err
}

// MarshalJSON returns the JSON encoding of the wrapped JSON value in j.
// Generally should only be used indirectly by json.Marshal when marshaling from
// a variable of type JSONValue.
func (j JSONValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(j.data)
}

// UnmarshalJSON sets the wrapped JSON value in j to the result of unmarshaling
// the given input data. Returns an error if the input data is not valid JSON.
// Generally should only used indirectly by json.Unmarshal when unmarshaling
// into a variable of type JSONValue.
func (j *JSONValue) UnmarshalJSON(data []byte) error {
	err := json.Unmarshal(data, &j.data)
	return err
}

// Equal returns true if other is deeply equal to j and false otherwise.
func (j JSONValue) Equal(other JSONValue) bool {
	return reflect.DeepEqual(j.data, other.data)
}

// Validate applies the given validation function to the wrapped JSON value in
// j. Returns an error if the validation function returns an error.
func (j JSONValue) Validate(v Validator) error {
	return v.Validate(j.data)
}
