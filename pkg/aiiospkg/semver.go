package aiiospkg

import (
	"regexp"
	"strings"
)

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

var reHostVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// .
func ValidHostVersion(v string) bool { return reHostVersion.MatchString(v) }

// .
func CompareHostVersion(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if c := compareDecimal(as[i], bs[i]); c != 0 {
			return c
		}
	}
	return 0
}

func compareDecimal(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}
