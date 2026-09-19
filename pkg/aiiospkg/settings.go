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

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
)

// .
const SettingsFile = "settings.json"

// .
const (
	MaxSettings           = 16
	MaxSettingKeyBytes    = 32
	MaxSettingTitleBytes  = 64
	MaxSettingDescBytes   = 256
	MaxSettingStringBytes = 1024
	MaxSettingEnumValues  = 256
	MaxSettingLabelBytes  = 64
)

// .
// .
// .
const (
	ScopeHearing  = "hearing"
	ScopeSpeaking = "speaking"
	ScopeSession  = "session"
)

// .
const (
	SettingString  = "string"
	SettingNumber  = "number"
	SettingInteger = "integer"
	SettingBoolean = "boolean"
	SettingEnum    = "enum"
	SettingSecret  = "secret"
)

var (
	reSettingKey    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	reSettingHandle = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
)

// .
// .
type SettingDecl struct {
	Key         string            `json:"key"`
	Type        string            `json:"type"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Default     interface{}       `json:"default,omitempty"`
	Values      []string          `json:"values,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Required    bool              `json:"required,omitempty"`
	Minimum     *float64          `json:"minimum,omitempty"`
	Maximum     *float64          `json:"maximum,omitempty"`
	// .
	// .
	// .
	Scope string `json:"scope,omitempty"`
	// .
	// .
	// .
	// .
	// .
	OAuth *SettingOAuthHint `json:"oauth,omitempty"`
}

// .
// .
type SettingOAuthHint struct {
	Provider string   `json:"provider"`
	Services []string `json:"services,omitempty"`
}

// .
func ParseSettings(raw []byte) ([]SettingDecl, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var decls []SettingDecl
	if err := dec.Decode(&decls); err != nil {
		return nil, fmt.Errorf("not a list of settings: %v", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("trailing content after the list")
	}
	if err := ValidateSettings(decls); err != nil {
		return nil, err
	}
	return decls, nil
}

// .
func ValidateSettings(decls []SettingDecl) error {
	if len(decls) > MaxSettings {
		return fmt.Errorf("%d settings; at most %d", len(decls), MaxSettings)
	}
	seen := map[string]bool{}
	for i, d := range decls {
		if !reSettingKey.MatchString(d.Key) {
			return fmt.Errorf("setting %d: key %q is not lowercase [a-z][a-z0-9_]{0,31}", i, d.Key)
		}
		if seen[d.Key] {
			return fmt.Errorf("setting %q declared twice", d.Key)
		}
		seen[d.Key] = true
		if d.Title == "" || len(d.Title) > MaxSettingTitleBytes {
			return fmt.Errorf("setting %q: title must be 1..%d bytes", d.Key, MaxSettingTitleBytes)
		}
		if len(d.Description) > MaxSettingDescBytes {
			return fmt.Errorf("setting %q: description over %d bytes", d.Key, MaxSettingDescBytes)
		}
		switch d.Type {
		case SettingString, SettingNumber, SettingInteger, SettingBoolean, SettingEnum, SettingSecret:
		default:
			return fmt.Errorf("setting %q: type %q is not string, number, integer, boolean, enum or secret", d.Key, d.Type)
		}
		switch d.Scope {
		case "", ScopeHearing, ScopeSpeaking, ScopeSession:
		default:
			return fmt.Errorf("setting %q: scope %q is not %s, %s or %s", d.Key, d.Scope, ScopeHearing, ScopeSpeaking, ScopeSession)
		}
		if d.OAuth != nil {
			if d.Type != SettingSecret {
				return fmt.Errorf("setting %q: an oauth hint belongs to a secret", d.Key)
			}
			if !reSettingHandle.MatchString(d.OAuth.Provider) {
				return fmt.Errorf("setting %q: oauth provider %q is not a name", d.Key, d.OAuth.Provider)
			}
			if len(d.OAuth.Services) > MaxSettingEnumValues {
				return fmt.Errorf("setting %q: at most %d oauth services", d.Key, MaxSettingEnumValues)
			}
			for _, svc := range d.OAuth.Services {
				if !reSettingKey.MatchString(svc) {
					return fmt.Errorf("setting %q: oauth service %q is not a name", d.Key, svc)
				}
			}
		}
		if d.Type == SettingEnum {
			if len(d.Values) == 0 || len(d.Values) > MaxSettingEnumValues {
				return fmt.Errorf("setting %q: an enum declares 1..%d values", d.Key, MaxSettingEnumValues)
			}
			vs := map[string]bool{}
			for _, v := range d.Values {
				if v == "" || len(v) > MaxSettingTitleBytes || vs[v] {
					return fmt.Errorf("setting %q: enum values are distinct, 1..%d bytes", d.Key, MaxSettingTitleBytes)
				}
				vs[v] = true
			}
			for v, label := range d.Labels {
				if !vs[v] {
					return fmt.Errorf("setting %q: label for %q, which is not one of its values", d.Key, v)
				}
				if label == "" || len(label) > MaxSettingLabelBytes {
					return fmt.Errorf("setting %q: labels are 1..%d bytes", d.Key, MaxSettingLabelBytes)
				}
			}
		} else {
			if len(d.Values) > 0 {
				return fmt.Errorf("setting %q: values belong to an enum", d.Key)
			}
			if len(d.Labels) > 0 {
				return fmt.Errorf("setting %q: labels belong to an enum", d.Key)
			}
		}
		numeric := d.Type == SettingNumber || d.Type == SettingInteger
		if !numeric && (d.Minimum != nil || d.Maximum != nil) {
			return fmt.Errorf("setting %q: minimum and maximum belong to a number", d.Key)
		}
		if d.Minimum != nil && d.Maximum != nil && *d.Minimum > *d.Maximum {
			return fmt.Errorf("setting %q: minimum above maximum", d.Key)
		}
		if d.Type == SettingInteger {
			for _, bound := range []*float64{d.Minimum, d.Maximum} {
				if bound != nil && !wholeNumber(*bound) {
					return fmt.Errorf("setting %q: an integer's bounds are whole numbers", d.Key)
				}
			}
		}
		if d.Type == SettingSecret && d.Default != nil {
			return fmt.Errorf("setting %q: a secret has no default; the operator names its handle", d.Key)
		}
		if d.Default != nil {
			if err := CheckSettingValue(d, d.Default); err != nil {
				return fmt.Errorf("setting %q: default %v", d.Key, err)
			}
		}
	}
	return nil
}

// .
func wholeNumber(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0) && f == math.Trunc(f)
}

// .
// .
func settingNumber(v interface{}) (float64, bool) {
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case float32:
		f = float64(n)
	case int:
		f = float64(n)
	case int64:
		f = float64(n)
	case json.Number:
		var err error
		if f, err = n.Float64(); err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// .
// .
func CheckSettingValue(d SettingDecl, v interface{}) error {
	switch d.Type {
	case SettingString:
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("must be a string")
		}
		if len(s) > MaxSettingStringBytes {
			return fmt.Errorf("over %d bytes", MaxSettingStringBytes)
		}
	case SettingNumber, SettingInteger:
		f, ok := settingNumber(v)
		if !ok {
			return fmt.Errorf("must be a number")
		}
		if d.Type == SettingInteger && !wholeNumber(f) {
			return fmt.Errorf("must be a whole number")
		}
		if d.Minimum != nil && f < *d.Minimum {
			return fmt.Errorf("below the minimum %v", *d.Minimum)
		}
		if d.Maximum != nil && f > *d.Maximum {
			return fmt.Errorf("above the maximum %v", *d.Maximum)
		}
	case SettingBoolean:
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("must be true or false")
		}
	case SettingEnum:
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("must be one of the declared %d values", len(d.Values))
		}
		for _, allowed := range d.Values {
			if s == allowed {
				return nil
			}
		}
		if len(d.Values) > 8 {
			return fmt.Errorf("must be one of the declared %d values; %q is not", len(d.Values), s)
		}
		return fmt.Errorf("must be one of %v", d.Values)
	case SettingSecret:
		s, ok := v.(string)
		if !ok || !reSettingHandle.MatchString(s) {
			return fmt.Errorf("must name a credential handle")
		}
	default:
		return fmt.Errorf("type %q is not declared", d.Type)
	}
	return nil
}

// .
// .
// .
func EffectiveSettings(decls []SettingDecl, values map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for _, d := range decls {
		if d.Default != nil {
			out[d.Key] = d.Default
		}
		if v, ok := values[d.Key]; ok && v != nil && CheckSettingValue(d, v) == nil {
			out[d.Key] = v
		}
	}
	return out
}

// .
// .
// .
func SettingsJSON(decls []SettingDecl) ([]byte, error) {
	if err := ValidateSettings(decls); err != nil {
		return nil, err
	}
	list := make([]interface{}, 0, len(decls))
	for _, d := range decls {
		entry := map[string]interface{}{"key": d.Key, "type": d.Type, "title": d.Title}
		if d.Description != "" {
			entry["description"] = d.Description
		}
		if d.Scope != "" {
			entry["scope"] = d.Scope
		}
		if d.Default != nil {
			entry["default"] = d.Default
		}
		if len(d.Values) > 0 {
			vals := make([]interface{}, 0, len(d.Values))
			for _, v := range d.Values {
				vals = append(vals, v)
			}
			entry["values"] = vals
		}
		if len(d.Labels) > 0 {
			labels := make(map[string]interface{}, len(d.Labels))
			for v, l := range d.Labels {
				labels[v] = l
			}
			entry["labels"] = labels
		}
		if d.Required {
			entry["required"] = true
		}
		if d.Minimum != nil {
			entry["minimum"] = *d.Minimum
		}
		if d.Maximum != nil {
			entry["maximum"] = *d.Maximum
		}
		list = append(list, entry)
	}
	return marshalCanonical(list)
}
