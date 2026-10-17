package aiiosdk

import "testing"

func TestArraysAreWalkedWithoutReflection(t *testing.T) {
	o := Object([]byte(`{"keys":["a","b c",""],"v":[1,2.5,-3e2],"m":[[1,2],[3]],"bad":[1,"x"],"s":"no"}`))
	if keys, ok := o.StringArray("keys"); !ok || len(keys) != 3 || keys[1] != "b c" {
		t.Fatalf("StringArray: %v %v", keys, ok)
	}
	if v, ok := o.FloatArray("v"); !ok || len(v) != 3 || v[1] != 2.5 || v[2] != -300 {
		t.Fatalf("FloatArray: %v %v", v, ok)
	}
	if m, ok := o.FloatMatrix("m"); !ok || len(m) != 2 || m[0][1] != 2 || len(m[1]) != 1 {
		t.Fatalf("FloatMatrix: %v %v", m, ok)
	}
	if _, ok := o.FloatArray("bad"); ok {
		t.Fatal("a string among numbers must not decode")
	}
	if _, ok := o.StringArray("s"); ok {
		t.Fatal("a string is not an array")
	}
	if _, ok := o.StringArray("absent"); ok {
		t.Fatal("absent is not an array")
	}
	if got := string(appendFloatArray(nil, []float32{1, 0.5, -3})); got != "[1,0.5,-3]" {
		t.Fatalf("appendFloatArray: %s", got)
	}
}

func TestObjectArrayWalksObjectsWithoutReflection(t *testing.T) {
	items, ok := ObjectArray([]byte(` [ {"name":"a.txt","dir":false,"size":3}, {"name":"{}\"]","dir":true} ] `))
	if !ok || len(items) != 2 {
		t.Fatalf("walk: ok=%v n=%d", ok, len(items))
	}
	if n, _ := items[0].String("name"); n != "a.txt" {
		t.Fatalf("first: %s", items[0])
	}
	if n, _ := items[1].String("name"); n != "{}\"]" {
		t.Fatalf("braces and quotes inside strings do not end an object: %s", items[1])
	}
	if _, ok := ObjectArray([]byte(`{"not":"an array"}`)); ok {
		t.Fatal("an object is not an array")
	}
	if _, ok := ObjectArray([]byte(`[{"unterminated":1`)); ok {
		t.Fatal("an unterminated object is refused")
	}
	if items, ok := ObjectArray([]byte(`[]`)); !ok || len(items) != 0 {
		t.Fatal("an empty array walks to nothing")
	}
}
