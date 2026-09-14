// .
// .
// .
// .
// .
package main

import (
	"fmt"
	"strings"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

const logFile = "events.log"

func init() {
	p := sdk.New("com.aiii.examples.logging")

	p.Describe("log.event", sdk.Descriptor{
		Summary:      "Record one delivered event as a line",
		Input:        "schemas/event_in.json",
		Output:       "schemas/event_out.json",
		Effects:      sdk.EffectsWriteLocal,
		Capabilities: []string{"fs.private"},
	})
	p.Handle("log.event", record)

	p.Describe("log.read", sdk.Descriptor{
		Summary:      "Read the most recent event lines",
		Input:        "schemas/read_in.json",
		Output:       "schemas/read_out.json",
		Effects:      sdk.EffectsReadInternal,
		Capabilities: []string{"fs.private"},
		Family:       "logging",
		Keywords:     []string{"log", "events", "audit", "what happened"},
	})
	p.Handle("log.read", read)

	p.Run()
}

func record(c sdk.Call) (any, error) {
	ev := sdk.ParseEvent(c)
	if ev.Topic == "" {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "log.event is delivered by the host with a topic")
	}
	var fields []string
	for _, k := range []string{"tool", "failed", "duration_ms", "event_type", "ring", "seq", "owner", "alarm_id", "accepted"} {
		if v, ok := ev.Payload().String(k); ok {
			fields = append(fields, k+"="+v)
			continue
		}
		if v, ok := ev.Payload().Int(k); ok {
			fields = append(fields, fmt.Sprintf("%s=%d", k, v))
			continue
		}
		if v, ok := ev.Payload().Bool(k); ok {
			fields = append(fields, fmt.Sprintf("%s=%v", k, v))
		}
	}
	line := ev.At + "\t" + ev.Topic + "\t" + strings.Join(fields, " ") + "\n"
	if err := sdk.Files.Append(sdk.PrivateRoot, logFile, []byte(line)); err != nil {
		return nil, err
	}
	return map[string]any{"recorded": true, "bytes": len(line)}, nil
}

func read(c sdk.Call) (any, error) {
	keep := int64(200)
	if vals, err := sdk.Settings.Load(); err == nil {
		if n, ok := vals.Int("keep_lines"); ok && n >= 10 && n <= 5000 {
			keep = n
		}
	}
	if n, ok := c.Args().Int("lines"); ok && n >= 1 && n < keep {
		keep = n
	}
	data, err := sdk.Files.ReadAll(sdk.PrivateRoot, logFile, 4<<20)
	if err != nil {
		if d, ok := sdk.AsDenied(err); ok {
			return nil, sdk.Deny(d.ReasonCode, "the host denied "+d.Message)
		}
		return map[string]any{"lines": []any{}, "total": 0}, nil
	}
	all := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(all) == 1 && all[0] == "" {
		all = nil
	}
	start := 0
	if int64(len(all)) > keep {
		start = len(all) - int(keep)
	}
	lines := make([]any, 0, len(all)-start)
	for _, l := range all[start:] {
		lines = append(lines, l)
	}
	return map[string]any{"lines": lines, "total": len(all)}, nil
}

// .
// .
func main() { sdk.MainDescribe() }
