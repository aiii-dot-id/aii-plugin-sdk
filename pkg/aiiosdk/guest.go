//go:build wasm_unknown

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
// .
// .
// .
// .
// .
// .
// .
// .

import "unsafe"

var (
	pinned        [][]byte
	pluginRetArea [8]byte
	importRetArea [8]byte
)

func pin(b []byte) {
	if len(b) > 0 {
		pinned = append(pinned, b)
	}
}

func releasePins() {
	// .
	// .
	for i := range pinned {
		pinned[i] = nil
	}
	pinned = pinned[:0]
}

//export cabi_realloc
func cabiRealloc(oldPtr, oldSize, align, newSize uintptr) uintptr {
	// .
	// .
	// .
	if newSize == 0 {
		return 0
	}
	b := make([]byte, newSize)
	if oldPtr != 0 && oldSize > 0 {
		n := oldSize
		if newSize < n {
			n = newSize
		}
		copy(b, unsafe.Slice((*byte)(unsafe.Pointer(oldPtr)), int(n)))
	}
	pin(b)
	return uintptr(unsafe.Pointer(&b[0]))
}

//export aiii-plugin-bbb-protocol-version
func abiProtocolVersion() uint32 {
	// .
	// .
	return 2
}

//export aiii-plugin-smoke
func abiSmoke() uint32 {
	// .
	// .
	return 1
}

//export plugin-invoke
func abiPluginInvoke(ptr, length uintptr) uintptr {
	var req []byte
	if length > 0 {
		req = unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(length))
	}
	out := respondCurrent(req)
	pin(out)
	var p uint32
	if len(out) > 0 {
		p = uint32(uintptr(unsafe.Pointer(&out[0])))
	}
	putLE32(pluginRetArea[0:4], p)
	putLE32(pluginRetArea[4:8], uint32(len(out)))
	return uintptr(unsafe.Pointer(&pluginRetArea[0]))
}

//export cabi_post_plugin-invoke
func abiPostPluginInvoke(_ uintptr) {
	// .
	// .
	// .
	releasePins()
}

//export aiii-plugin-describe
func abiPluginDescribe() uintptr {
	// .
	// .
	// .
	// .
	// .
	// .
	// .
	// .
	var out []byte
	if current != nil {
		if b, err := current.DescriptorsJSON(); err == nil {
			out = b
		}
	}
	pin(out)
	var p uint32
	if len(out) > 0 {
		p = uint32(uintptr(unsafe.Pointer(&out[0])))
	}
	putLE32(pluginRetArea[0:4], p)
	putLE32(pluginRetArea[4:8], uint32(len(out)))
	return uintptr(unsafe.Pointer(&pluginRetArea[0]))
}

//export cabi_post_aiii-plugin-describe
func abiPostPluginDescribe(_ uintptr) {
	releasePins()
}

//export on_event
func abiOnEvent(topicPtr, topicLen, payloadPtr, payloadLen uintptr) {
	// .
	// .
	// .
	// .
	var topic string
	if topicLen > 0 {
		topic = string(unsafe.Slice((*byte)(unsafe.Pointer(topicPtr)), int(topicLen)))
	}
	var payload []byte
	if payloadLen > 0 {
		payload = append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(payloadPtr)), int(payloadLen))...)
	}
	dispatchEvent(topic, payload)
	// .
	// .
	// .
	releasePins()
}

// .
// .
// .
// .
// .

//go:wasmimport aiii:bbb/bbb invoke-call
func bbbInvokeCall(paramsPtr unsafe.Pointer, paramsLen uint32, retPtr unsafe.Pointer)

// .
// .
// .
// .
func hostInvokeCallRaw(params []byte) ([]byte, error) {
	var pp unsafe.Pointer
	if len(params) > 0 {
		pp = unsafe.Pointer(&params[0])
	}
	bbbInvokeCall(pp, uint32(len(params)), unsafe.Pointer(&importRetArea[0]))
	replyPtr := getLE32(importRetArea[0:4])
	replyLen := getLE32(importRetArea[4:8])
	if replyLen == 0 {
		return nil, nil
	}
	reply := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(replyPtr))), int(replyLen))
	return append([]byte(nil), reply...), nil
}

func putLE32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func getLE32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
