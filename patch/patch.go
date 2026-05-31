package patch

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
)

// PatchApplierImpl is an implementation of PatchApplier.
type PatchApplierImpl struct{}

// getOrCreateObject retrieves or creates the nested object at the specified path within docMap.
func getOrCreateObject(doc map[string]interface{}, pathParts []string) (map[string]interface{}, error) {
	current := doc

	for i, part := range pathParts {
		// Check if the current part is an array index
		if idx, isArrayIndex := parseArrayIndex(part); isArrayIndex {
			// Ensure that the previous part is an array
			if array, ok := current[pathParts[i-1]].([]interface{}); ok {
				// Ensure the array index is within bounds
				if idx >= len(array) || idx < 0 {
					return nil, fmt.Errorf("array index %d exceeds array length %d", idx, len(array))
				}

				// Check if the element at this index is a map, meaning an object
				if obj, ok := array[idx].(map[string]interface{}); ok {
					current = obj
				} else {
					return nil, fmt.Errorf("found non-object type at array index %d", idx)
				}
			} else {
				return nil, fmt.Errorf("path includes a non-array element where an array was expected")
			}
		} else {
			// Handle standard object traversal for non-array parts
			next, exists := current[part]
			if !exists {
				// Create a new object if it doesn't exist
				newObj := make(map[string]interface{})
				current[part] = newObj
				current = newObj
			} else if obj, ok := next.(map[string]interface{}); ok {
				current = obj
			} else if arr, ok := next.([]interface{}); ok {
				// If it’s an array, continue expecting indices in the subsequent parts
				current[pathParts[i]] = arr
			} else {
				return nil, fmt.Errorf("path includes a non-object element")
			}
		}
	}

	return current, nil
}

// Helper function to parse and validate JSON-compatible value types.
func parseValue(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		var parsed interface{}
		// Attempt to parse the string as JSON
		if err := json.Unmarshal([]byte(v), &parsed); err == nil {
			// Check if parsed is a valid JSON object, array, or primitive type
			if _, ok := parsed.(map[string]interface{}); ok {
				return parsed, nil
			}
			if _, ok := parsed.([]interface{}); ok {
				return parsed, nil
			}
			if isValidJSONPrimitive(parsed) {
				return parsed, nil
			}
			return nil, fmt.Errorf("unsupported JSON type: %T", parsed)
		}
		// If the string is not valid JSON, treat it as a plain string
		return v, nil
	case int, float64, bool, map[string]interface{}, []interface{}:
		// Directly return valid JSON-compatible types
		return v, nil
	default:
		return nil, fmt.Errorf("unsupported value type: %T", v)
	}
}

// Helper function to check if a parsed JSON value is a valid primitive.
func isValidJSONPrimitive(v interface{}) bool {
	switch v.(type) {
	case string, float64, int, bool:
		return true
	default:
		return false
	}
}

// parseArrayIndex tries to parse a string into an array index.
func parseArrayIndex(part string) (int, bool) {
	idx, err := strconv.Atoi(part)
	if err != nil {
		return 0, false
	}
	return idx, true
}

// GetArray is a helper function to navigate to the array at the specified path within docMap.
func GetArray(doc map[string]interface{}, path string) ([]interface{}, error) {
	slog.Info("Getting array")
	// Split the path into parts
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	var current interface{} = doc

	for i, part := range parts {
		slog.Info("Processing part", "part", part)
		// Check if the part is an array index
		if idx, isIndex := parseArrayIndex(part); isIndex {
			slog.Info("Processing index", "idx", idx)
			// Ensure current is an array
			if array, ok := current.([]interface{}); ok {
				if idx < 0 || idx >= len(array) {
					return nil, fmt.Errorf("array index %d exceeds array length %d", idx, len(array))
				}
				// If it's the last part, return the array at the index
				if i == len(parts)-1 {
					if nestedArray, ok := array[idx].([]interface{}); ok {
						return nestedArray, nil
					}
					return nil, fmt.Errorf("path does not lead to an array")
				}
				// Move to the next nested element
				current = array[idx]
			} else {
				return nil, fmt.Errorf("path includes a non-array element")
			}
		} else {
			// If it's the last part, ensure it points to an array
			if i == len(parts)-1 {
				if array, ok := current.(map[string]interface{})[part].([]interface{}); ok {
					return array, nil
				}
				return nil, fmt.Errorf("path does not lead to an array")
			}

			// Move to the next map element in the path
			if next, ok := current.(map[string]interface{})[part]; ok {
				current = next
			} else {
				return nil, fmt.Errorf("path not found or does not point to an object")
			}
		}
	}
	return nil, fmt.Errorf("path not found")
}

// SetArray sets the modified array back to the document at the specified path.
func SetArray(doc map[string]interface{}, path string, array []interface{}) error {
	slog.Info("Setting array")
	// Split the path into parts
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	var current interface{} = doc

	for i, part := range parts {
		slog.Info("Processing part", "part", part)
		// Check if the part is an array index
		if idx, isIndex := parseArrayIndex(part); isIndex {
			slog.Info("Processing index", "idx", idx)
			// Ensure current is an array
			if arrayElem, ok := current.([]interface{}); ok {
				if idx < 0 || idx >= len(arrayElem) {
					return fmt.Errorf("array index %d exceeds array length %d", idx, len(arrayElem))
				}
				// If it's the last part, set the array at the index
				if i == len(parts)-1 {
					arrayElem[idx] = array
					return nil
				}
				// Move to the next nested element
				current = arrayElem[idx]
			} else {
				return fmt.Errorf("path includes a non-array element")
			}
		} else {
			// If it's the last part, set the array directly
			if i == len(parts)-1 {
				current.(map[string]interface{})[part] = array
				return nil
			}

			// Move to the next map element in the path
			if next, ok := current.(map[string]interface{})[part]; ok {
				current = next
			} else {
				return fmt.Errorf("path not found or does not point to an object")
			}
		}
	}
	return fmt.Errorf("path not found")
}

// ApplyObjectAdd adds a key-value pair to an object at the specified path within docMap if the key does not already exist.
func (p *PatchApplierImpl) ApplyObjectAdd(docMap map[string]interface{}, path string, value interface{}) (bool, []byte, string) {
	slog.Info("Starting ApplyObjectAdd")
	// Check if the path is empty
	if path == "" {
		return true, nil, "error applying patches: path ends in object"
	}

	// Validate the value
	convertedValue, err := parseValue(value)
	if err != nil {
		slog.Info("Error parsing value", "error", err)
		return true, nil, fmt.Sprintf("Invalid patches: %v", err.Error())
	}

	// Split path to navigate through the document structure
	pathParts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	slog.Info("Path parts", "pathParts", pathParts)

	// Navigate to the target object in the document
	slog.Info("Navigating to target object")
	target, err := getOrCreateObject(docMap, pathParts[:len(pathParts)-1])
	if err != nil {
		return true, nil, err.Error()
	}

	// Extract the last element of the path as the key
	key := pathParts[len(pathParts)-1]
	slog.Info("Target object", "target", target)

	// Check if the key already exists, do nothing if it does
	if _, exists := target[key]; exists {
		modifiedData, _ := json.Marshal(docMap)
		return false, modifiedData, "patch applied"
	}

	// Add key-value pair to the object
	slog.Info("Adding key-value pair", "key", key, "value", convertedValue)
	target[key] = convertedValue

	// Marshal modified document back to JSON
	slog.Info("Marshalling modified document")
	modifiedData, err := json.Marshal(docMap)
	if err != nil {
		return true, nil, fmt.Sprintf("Error marshalling modified document data: %v", err)
	}
	slog.Info("ApplyObjectAdd completed successfully")
	return false, modifiedData, "patch applied"
}

// ApplyArrayAdd adds a value to an array at a specified path if it is not already present.
func (p *PatchApplierImpl) ApplyArrayAdd(docMap map[string]interface{}, path string, value interface{}) (bool, []byte, string) {
	// Check if the path is empty
	if path == "" {
		return true, nil, "error applying patches: path ends in object"
	}

	// Parse and validate the value to be added
	parsedValue, err := parseValue(value)
	if err != nil {
		return true, nil, fmt.Sprintf("invalid patches: %v", err)
	}

	// Retrieve the array from the document at the specified path
	array, err := GetArray(docMap, path)
	if err != nil {
		return true, nil, err.Error()
	}
	slog.Info("array", "array", array)

	// Check if the value already exists in the array
	for _, item := range array {
		if reflect.DeepEqual(item, parsedValue) {
			// If the item is already present, return with no changes
			modifiedData, _ := json.Marshal(docMap)
			return false, modifiedData, "patch applied"
		}
	}

	// Add the parsed value to the array
	array = append(array, parsedValue)

	// Write the modified array back to the document structure
	if err := SetArray(docMap, path, array); err != nil {
		return true, nil, err.Error()
	}

	// Marshal the modified docMap back to JSON bytes
	modifiedData, err := json.Marshal(docMap)
	if err != nil {
		return true, nil, fmt.Sprintf("Error marshalling modified document data: %v", err)
	}

	return false, modifiedData, "patch applied"
}

// ApplyArrayRemove removes a value from an array at a specified path in the provided docMap if it exists.
func (p *PatchApplierImpl) ApplyArrayRemove(docMap map[string]interface{}, path string, value interface{}) (bool, []byte, string) {
	// Check if the path is empty
	if path == "" {
		return true, nil, "error applying patches: path ends in object"
	}

	// Parse and validate the input value to ensure it's JSON-compatible
	parsedValue, err := parseValue(value)
	if err != nil {
		return true, nil, fmt.Sprintf("Invalid value for ArrayRemove: %v", err)
	}

	// Get the array at the specified path
	array, err := GetArray(docMap, path)
	if err != nil {
		return true, nil, err.Error()
	}

	// Find and remove the value from the array
	modified := false
	for i, item := range array {
		// Directly compare parsedValue and item; this can be enhanced if necessary to support nested structures
		if reflect.DeepEqual(item, parsedValue) {
			// Remove the item from the array
			array = append(array[:i], array[i+1:]...)
			modified = true
			break
		}
	}

	// If the value wasn't found, no changes are made to the document
	if !modified {
		modifiedData, _ := json.Marshal(docMap) // Marshal the original document since there's no change
		return false, modifiedData, "patch applied"
	}

	// Set the modified array back in the document
	if err := SetArray(docMap, path, array); err != nil {
		return true, nil, err.Error()
	}

	// Marshal the modified docMap back to JSON bytes
	modifiedData, err := json.Marshal(docMap)
	if err != nil {
		return true, nil, fmt.Sprintf("Error marshalling modified document data: %v", err)
	}

	return false, modifiedData, "patch applied"
}

// NewPatchApplier creates a new instance of PatchApplierImpl.
func NewPatchApplier() *PatchApplierImpl {
	return &PatchApplierImpl{}
}
