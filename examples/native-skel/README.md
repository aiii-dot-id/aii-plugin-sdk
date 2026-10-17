# native-skel

The smallest native plugin: a process the host runs and speaks to over
framed stdio, with the same handlers a WASM plugin has and `p.Serve` as
its last line. One package carries one variant per desktop platform —
`build.sh` cross-compiles all three — and the host selects its own.

    ./build.sh && aiisdk package

Native code runs only at T3, the platform's tier, so a host refuses the
package this builds until the platform signs it. Write the plugin as
though the sandbox wall is there: every effect goes through the broker.
