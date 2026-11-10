package aiiosdk

import (
	"sync/atomic"
	"time"
)

func (c Call) HostNowMillis() (int64, bool) {
	return c.Args().Int("_host_now_ms")
}

func LastHostTime() (time.Time, bool) {
	n := lastHostTime.Load()
	if n == 0 {
		return time.Time{}, false
	}
	return time.Unix(0, n).UTC(), true
}

var lastHostTime atomic.Int64

func receiptTime(receipt []byte) (time.Time, bool) {
	if len(receipt) == 0 {
		return time.Time{}, false
	}
	s, ok := Object(receipt).String("timestamp")
	if !ok || s == "" {
		return time.Time{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || at.UnixNano() <= 0 {
		return time.Time{}, false
	}
	return at, true
}
