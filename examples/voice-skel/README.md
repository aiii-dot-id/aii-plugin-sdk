# voice-skel

The shape of a resident speech engine: `p.ServeSession` runs the
full-duplex session lane, the eight controls are admitted in order and
answered at once, and everything the host learns arrives as an event
with the session's identity and a contiguous sequence. No models, no
audio — the contract, whole (WRITING_A_PLUGIN.md, section 17).

    go test .                        # the contract, driven over the real lane
    ./build.sh && aiisdk package     # one native package per desktop platform

An `open` may carry test knobs (`test_close_delay_ms`, `test_synth_ms`,
`test_telemetry_burst`, `test_critical_burst`, `test_die_after_ms`) so a
host can prove its own side against the engine. Native code runs only
at T3, the platform's tier.
