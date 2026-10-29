package main

import (
	"fmt"
	"strings"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

const logFile = "events.log"

const defaultMaxReadBytes = 4 << 20

const logTooLargeMessage = "the log holds more than the %d bytes log.read is set to read (the max_read_bytes setting); nothing was read and nothing was removed"

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
	maxRead := defaultMaxReadBytes
	if vals, err := sdk.Settings.Load(); err == nil {
		if n, ok := vals.Int("keep_lines"); ok && n >= 10 && n <= 5000 {
			keep = n
		}
		if n, ok := vals.Int("max_read_bytes"); ok && n >= 64<<10 && n <= 16<<20 {
			maxRead = int(n)
		}
	}
	if n, ok := c.Args().Int("lines"); ok && n >= 1 && n < keep {
		keep = n
	}

	data, err := sdk.Files.ReadAll(sdk.PrivateRoot, logFile, maxRead)
	if err != nil {
		if oe, ok := sdk.AsOperationError(err); ok && oe.ReasonCode == "FS_NOT_FOUND" {
			return map[string]any{"lines": []any{}, "total": 0}, nil
		}
		if d, ok := sdk.AsDenied(err); ok {
			return nil, sdk.Deny(d.ReasonCode, "the host denied "+d.Message)
		}

		if len(data) > maxRead {
			return nil, sdk.Fail("LOG_TOO_LARGE", fmt.Sprintf(logTooLargeMessage, maxRead))
		}
		return nil, err
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

func main() { sdk.MainDescribe() }
