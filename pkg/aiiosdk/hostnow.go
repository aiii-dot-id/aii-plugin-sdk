package aiiosdk

// .
// .
// .
// .
// .
// .
// .
// .
// .
func (c Call) HostNowMillis() (int64, bool) {
	return c.Args().Int("_host_now_ms")
}
