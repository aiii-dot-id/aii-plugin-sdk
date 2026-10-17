# memory-skel

The smallest guest: three operations over the brokered key-value store
— `focus.echo` returns its arguments, `focus.set` and `focus.get` keep
one value under a key — and a denial reported as an honest result when
the operator has not granted `ring4.kv`.

    ./build.sh

It builds with TinyGo alone (`memory-skel.wasm`) and is the guest the
kit's own e2e proof runs on a host. To start a plugin of your own, use
`aiisdk init`.
