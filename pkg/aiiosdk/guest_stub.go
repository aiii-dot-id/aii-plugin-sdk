//go:build !wasm_unknown

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
// .
// .
var hostInvokeStub func(params []byte) ([]byte, error)

func hostInvokeCallRaw(params []byte) ([]byte, error) {
	if hostInvokeStub != nil {
		return hostInvokeStub(params)
	}
	// .
	// .
	// .
	if t := nativeTransport.Load(); t != nil {
		return t.hostInvokeNative(params)
	}
	return nil, ErrNotInGuest
}
