package aiiosdk

import "fmt"

// .
// .
// .
// .
// .
// .
// .
// .
var Settings SettingsClient

// .
type SettingsClient struct{}

// .
type Values struct {
	obj Object
}

// .
// .
// .
func (SettingsClient) Load() (Values, error) {
	res, err := InvokeCall("settings.get", nil, nil)
	if err != nil {
		return Values{}, err
	}
	obj := Object(res.OperationResult).Object("values")
	if obj == nil {
		return Values{}, fmt.Errorf("aiiosdk: settings.get result carries no values object")
	}
	return Values{obj: obj}, nil
}

// .
// .
func (v Values) Has(key string) bool { return v.obj != nil && v.obj.Has(key) }

// .
func (v Values) String(key string) (string, bool) {
	if v.obj == nil {
		return "", false
	}
	return v.obj.String(key)
}

// .
func (v Values) Handle(key string) (string, bool) { return v.String(key) }

// .
func (v Values) Number(key string) (float64, bool) {
	if v.obj == nil {
		return 0, false
	}
	return v.obj.Float(key)
}

// .
// .
// .
func (v Values) Int(key string) (int64, bool) {
	if v.obj == nil {
		return 0, false
	}
	return v.obj.Int(key)
}

// .
func (v Values) Bool(key string) (bool, bool) {
	if v.obj == nil {
		return false, false
	}
	return v.obj.Bool(key)
}
