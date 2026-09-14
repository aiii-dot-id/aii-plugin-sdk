package aiiospkg

// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
)

// .
type SchemaSubsetError struct {
	Path    string
	Keyword string
	Reason  string
}

func (e *SchemaSubsetError) Error() string {
	return fmt.Sprintf("schema %s: keyword %q: %s", e.Path, e.Keyword, e.Reason)
}

var schemaAnnotations = map[string]bool{
	"$schema": true, "$id": true, "title": true, "description": true,
	"default": true, "examples": true, "deprecated": true, "format": true,
}

var schemaTypeNames = map[string]bool{
	"object": true, "array": true, "string": true, "number": true,
	"integer": true, "boolean": true, "null": true,
}

const schemaMaxDepth = 32

// .
// .
// .
func CheckSchemaSubset(raw []byte) error {
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return &SchemaSubsetError{Path: "#", Keyword: "", Reason: "not a JSON object: " + err.Error()}
	}
	return checkSchemaNode(doc, "#", 0)
}

func checkSchemaNode(doc map[string]interface{}, path string, depth int) error {
	if depth > schemaMaxDepth {
		return &SchemaSubsetError{Path: path, Reason: fmt.Sprintf("nested deeper than %d", schemaMaxDepth)}
	}
	for k, v := range doc {
		if schemaAnnotations[k] {
			continue
		}
		switch k {
		case "type":
			switch t := v.(type) {
			case string:
				if !schemaTypeNames[t] {
					return &SchemaSubsetError{Path: path, Keyword: k, Reason: fmt.Sprintf("unknown type %q", t)}
				}
			case []interface{}:
				if len(t) == 0 {
					return &SchemaSubsetError{Path: path, Keyword: k, Reason: "empty type list"}
				}
				for _, e := range t {
					name, ok := e.(string)
					if !ok || !schemaTypeNames[name] {
						return &SchemaSubsetError{Path: path, Keyword: k, Reason: fmt.Sprintf("unknown type %v", e)}
					}
				}
			default:
				return &SchemaSubsetError{Path: path, Keyword: k, Reason: "must be a type name or a list of type names"}
			}
		case "properties":
			m, ok := v.(map[string]interface{})
			if !ok {
				return &SchemaSubsetError{Path: path, Keyword: k, Reason: "must be an object of schemas"}
			}
			for name, sub := range m {
				subm, ok := sub.(map[string]interface{})
				if !ok {
					return &SchemaSubsetError{Path: path + "/properties/" + name, Keyword: k, Reason: "each property must be a schema object"}
				}
				if err := checkSchemaNode(subm, path+"/properties/"+name, depth+1); err != nil {
					return err
				}
			}
		case "required":
			list, ok := v.([]interface{})
			if !ok {
				return &SchemaSubsetError{Path: path, Keyword: k, Reason: "must be an array of property names"}
			}
			for _, e := range list {
				if name, ok := e.(string); !ok || name == "" {
					return &SchemaSubsetError{Path: path, Keyword: k, Reason: "must list non-empty property names"}
				}
			}
		case "additionalProperties", "uniqueItems":
			if _, ok := v.(bool); !ok {
				return &SchemaSubsetError{Path: path, Keyword: k, Reason: "must be a boolean in the closed subset"}
			}
		case "items":
			m, ok := v.(map[string]interface{})
			if !ok {
				return &SchemaSubsetError{Path: path, Keyword: k, Reason: "must be one schema object in the closed subset"}
			}
			if err := checkSchemaNode(m, path+"/items", depth+1); err != nil {
				return err
			}
		case "enum":
			if list, ok := v.([]interface{}); !ok || len(list) == 0 {
				return &SchemaSubsetError{Path: path, Keyword: k, Reason: "must be a non-empty array"}
			}
		case "const":
		case "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf":
			f, ok := v.(float64)
			if !ok {
				return &SchemaSubsetError{Path: path, Keyword: k, Reason: "must be a number"}
			}
			if k == "multipleOf" && f <= 0 {
				return &SchemaSubsetError{Path: path, Keyword: k, Reason: "must be greater than zero"}
			}
		case "minLength", "maxLength", "minItems", "maxItems", "minProperties", "maxProperties":
			f, ok := v.(float64)
			if !ok || f != math.Trunc(f) || f < 0 || f > math.MaxInt32 {
				return &SchemaSubsetError{Path: path, Keyword: k, Reason: "must be a non-negative integer"}
			}
		case "pattern":
			p, ok := v.(string)
			if !ok {
				return &SchemaSubsetError{Path: path, Keyword: k, Reason: "must be a string"}
			}
			if _, err := regexp.Compile(p); err != nil {
				return &SchemaSubsetError{Path: path, Keyword: k, Reason: "not RE2 syntax: " + err.Error()}
			}
		default:
			return &SchemaSubsetError{Path: path, Keyword: k, Reason: "outside the closed subset"}
		}
	}
	return nil
}
