package aiiosdk

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

const (
	SendLimitSetting  = "send_limit"
	SendWindowSetting = "send_window_minutes"

	DefaultSendLimit         = 20
	DefaultSendWindowMinutes = 60
	MaxSendLimit             = 1000
	MaxSendWindowMinutes     = 1440

	sendClockSkew = time.Minute
)

type SendWindow struct{ Key string }

type SendVerdict struct {
	OK            bool
	Sent          int
	Limit         int64
	WindowMinutes int64
	RetryAt       time.Time
}

func windowFailed(format string, a ...any) *OperationError {
	return Fail("NET_REMOTE_FAILED", fmt.Sprintf(format, a...))
}

func windowKV(err error) error {
	if d, ok := AsDenied(err); ok {
		return Deny(d.ReasonCode, "the plugin's storage is not granted here — grant kv to this plugin on the Plugins page; it keeps the times of its recent sends, which its send limit is counted from — the message was not sent")
	}
	return windowFailed("the plugin's storage could not be read or written (%v) — the message was not sent", err)
}

func windowSettings() (limit, minutes int64, err error) {
	vals, err := Settings.Load()
	if err != nil {
		return 0, 0, windowFailed("the plugin's settings could not be read (%v) — the message was not sent", err)
	}
	limit, minutes = DefaultSendLimit, DefaultSendWindowMinutes
	if v, ok := vals.Int(SendLimitSetting); ok {
		limit = v
	}
	if v, ok := vals.Int(SendWindowSetting); ok {
		minutes = v
	}
	if limit < 1 || limit > MaxSendLimit {
		return 0, 0, windowFailed("the %s setting is %d; it is a whole number 1 to %d — the message was not sent", SendLimitSetting, limit, MaxSendLimit)
	}
	if minutes < 1 || minutes > MaxSendWindowMinutes {
		return 0, 0, windowFailed("the %s setting is %d; it is a whole number 1 to %d — the message was not sent", SendWindowSetting, minutes, MaxSendWindowMinutes)
	}
	return limit, minutes, nil
}

func (w SendWindow) recent(now, minutes int64) ([]int64, error) {
	raw, found, err := KV.Get(w.Key)
	if err != nil {
		return nil, windowKV(err)
	}
	var times []int64
	if !found || json.Unmarshal([]byte(raw), &times) != nil {
		return nil, nil
	}
	from, to := now-minutes*60_000, now+sendClockSkew.Milliseconds()
	kept := times[:0]
	for _, t := range times {
		if t > from && t <= to {
			kept = append(kept, t)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i] < kept[j] })
	return kept, nil
}

func (w SendWindow) Admit(c Call) (SendVerdict, error) {
	limit, minutes, err := windowSettings()
	if err != nil {
		return SendVerdict{}, err
	}
	now, ok := c.HostNowMillis()
	if !ok {
		return SendVerdict{}, windowFailed("the host gave this call no clock, so the send window cannot be kept — the message was not sent")
	}
	recent, err := w.recent(now, minutes)
	if err != nil {
		return SendVerdict{}, err
	}
	v := SendVerdict{OK: int64(len(recent)) < limit, Sent: len(recent), Limit: limit, WindowMinutes: minutes}
	if !v.OK {
		v.RetryAt = time.UnixMilli(recent[len(recent)-int(limit)] + minutes*60_000).UTC()
	}
	return v, nil
}

func (SendWindow) Counts(err error) bool { return err == nil || EffectUnknown(err) }

func (w SendWindow) Record(c Call, v SendVerdict) error {
	now, ok := c.HostNowMillis()
	if !ok {
		return fmt.Errorf("the host gave this call no clock")
	}
	recent, err := w.recent(now, v.WindowMinutes)
	if err != nil {
		return err
	}
	recent = append(recent, now)
	if len(recent) > MaxSendLimit {
		recent = recent[len(recent)-MaxSendLimit:]
	}
	raw, _ := json.Marshal(recent)
	if _, err := KV.Put(w.Key, string(raw)); err != nil {
		return windowKV(err)
	}
	return nil
}

func (v SendVerdict) Refusal(didNot string) *OperationError {
	return windowFailed("this channel has sent %d in the last %d minutes, its limit — the operator sets it as this plugin's %s and %s settings; the next send can go at %s — %s",
		v.Sent, v.WindowMinutes, SendLimitSetting, SendWindowSetting, v.RetryAt.Format(time.RFC3339), didNot)
}
