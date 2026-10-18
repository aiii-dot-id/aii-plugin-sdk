package aiiospkg

import (
	"fmt"
)

const ScheduleFile = "schedule.json"

const (
	MaxScheduled = 4

	MinScheduleEvery = 600

	MaxScheduleEvery = 7 * 24 * 3600
)

type ScheduleDecl struct {
	Operation    string `json:"operation"`
	EverySeconds int    `json:"every_seconds"`
}

func ValidateSchedule(decls []ScheduleDecl) error {
	if len(decls) > MaxScheduled {
		return fmt.Errorf("%d scheduled operations; at most %d", len(decls), MaxScheduled)
	}
	named := map[string]bool{}
	for _, d := range decls {
		switch {
		case d.Operation == "":
			return fmt.Errorf("a scheduled operation names its operation")
		case named[d.Operation]:
			return fmt.Errorf("operation %q is scheduled twice", d.Operation)
		case d.EverySeconds < MinScheduleEvery || d.EverySeconds > MaxScheduleEvery:
			return fmt.Errorf("operation %q: every_seconds must be %d..%d (ten minutes to a week)", d.Operation, MinScheduleEvery, MaxScheduleEvery)
		}
		named[d.Operation] = true
	}
	return nil
}

func CheckScheduleOperations(decls []ScheduleDecl, operations []string) error {
	known := map[string]bool{}
	for _, op := range operations {
		known[op] = true
	}
	for _, d := range decls {
		if !known[d.Operation] {
			return fmt.Errorf("scheduled operation %q is not one this plugin describes", d.Operation)
		}
	}
	return nil
}

func ScheduleJSON(decls []ScheduleDecl) ([]byte, error) {
	if err := ValidateSchedule(decls); err != nil {
		return nil, err
	}
	list := make([]interface{}, 0, len(decls))
	for _, d := range decls {
		list = append(list, map[string]interface{}{"operation": d.Operation, "every_seconds": d.EverySeconds})
	}
	return marshalCanonical(list)
}
