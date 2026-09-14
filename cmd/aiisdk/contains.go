package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// .
// .
// .
// .
// .
// .
// .
type containsList []string

func (c *containsList) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		if one == "" {
			*c = nil
		} else {
			*c = containsList{one}
		}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return fmt.Errorf("result_contains: a string or an array of strings")
	}
	*c = containsList(many)
	return nil
}

// .
// .
func (c containsList) missingFrom(payload string) string {
	for _, want := range c {
		if want != "" && !strings.Contains(payload, want) {
			return want
		}
	}
	return ""
}

// .
func (c containsList) String() string {
	if len(c) == 1 {
		return fmt.Sprintf("%q", c[0])
	}
	return fmt.Sprintf("%q", []string(c))
}
