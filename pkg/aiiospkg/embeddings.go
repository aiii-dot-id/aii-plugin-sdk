package aiiospkg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	EmbeddingsInterfaceID = "aii.embeddings"

	EmbeddingsInterfaceVersion = 1

	EmbeddingsFamily = "provider_bridge"

	MethodEmbed = "embed"

	EmbeddingsFile = "embeddings.json"

	EmbeddingsMinHost = "0.1.15"

	EmbedKindMemory = "memory"
	EmbedKindCue    = "cue"

	MaxEmbeddingDimension = 4096

	MaxEmbeddingForm = 65535

	MaxEmbeddingProbes = 32

	MaxEmbedChars = 8000

	MinProbeBound = 0.9
)

type EmbeddingsDecl struct {
	Dimension int `json:"dimension"`

	Form int `json:"form"`

	Floor float64 `json:"floor"`

	Model string `json:"model"`

	ProbeBound float64 `json:"probe_bound"`

	Probes []EmbeddingProbe `json:"probes"`
}

type EmbeddingProbe struct {
	Kind   string    `json:"kind"`
	Text   string    `json:"text"`
	Vector []float64 `json:"vector"`
}

func ParseEmbeddings(raw []byte) (*EmbeddingsDecl, error) {
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("not an embeddings declaration: the file is not valid UTF-8")
	}
	if err := validateUnicodeEscapes(raw); err != nil {
		return nil, fmt.Errorf("not an embeddings declaration: a string holds an escape that names no character")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var d EmbeddingsDecl
	err := readObject(dec, "the declaration", func(key string) (bool, error) {
		switch key {
		case "dimension":
			return true, readInt(dec, key, &d.Dimension)
		case "form":
			return true, readInt(dec, key, &d.Form)
		case "floor":
			return true, readFloat(dec, key, &d.Floor)
		case "probe_bound":
			return true, readFloat(dec, key, &d.ProbeBound)
		case "model":
			return true, readString(dec, key, &d.Model)
		case "probes":
			return true, readList(dec, key, func() error {
				var p EmbeddingProbe
				err := readObject(dec, "a probe", func(key string) (bool, error) {
					switch key {
					case "kind":
						return true, readString(dec, key, &p.Kind)
					case "text":
						return true, readString(dec, key, &p.Text)
					case "vector":
						return true, readList(dec, key, func() error {
							var v float64
							if err := readFloat(dec, "a vector's entry", &v); err != nil {
								return err
							}
							p.Vector = append(p.Vector, v)
							return nil
						})
					}
					return false, nil
				})
				d.Probes = append(d.Probes, p)
				return err
			})
		}
		return false, nil
	})
	if err != nil {
		return nil, fmt.Errorf("not an embeddings declaration: %v", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing content after the embeddings declaration")
	}
	if err := ValidateEmbeddings(&d); err != nil {
		return nil, err
	}
	return &d, nil
}

func readObject(dec *json.Decoder, what string, member func(key string) (bool, error)) error {
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return fmt.Errorf("%s must be an object", what)
	}
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := tok.(string)
		if seen[key] {
			return fmt.Errorf("%s names %q twice", what, key)
		}
		seen[key] = true
		known, err := member(key)
		if err != nil {
			return err
		}
		if !known {
			return fmt.Errorf("%s has no member %q (a member's name must match exactly)", what, key)
		}
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	return nil
}

func readList(dec *json.Decoder, what string, item func() error) error {
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return fmt.Errorf("%s must be a list", what)
	}
	for dec.More() {
		if err := item(); err != nil {
			return err
		}
	}
	_, err := dec.Token()
	return err
}

func readString(dec *json.Decoder, what string, out *string) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	s, ok := tok.(string)
	if !ok {
		return fmt.Errorf("%s must be a string", what)
	}
	*out = s
	return nil
}

func readFloat(dec *json.Decoder, what string, out *float64) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	n, ok := tok.(json.Number)
	if !ok {
		return fmt.Errorf("%s must be a number", what)
	}
	f, err := n.Float64()
	if err != nil {
		return fmt.Errorf("%s must be a number", what)
	}
	*out = f
	return nil
}

func readInt(dec *json.Decoder, what string, out *int) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	n, ok := tok.(json.Number)
	if !ok {
		return fmt.Errorf("%s must be a whole number", what)
	}
	i, err := n.Int64()
	if err != nil || int64(int(i)) != i {
		return fmt.Errorf("%s must be a whole number", what)
	}
	*out = int(i)
	return nil
}

func ValidateEmbeddings(d *EmbeddingsDecl) error {
	switch {
	case d.Dimension < 1 || d.Dimension > MaxEmbeddingDimension:
		return fmt.Errorf("dimension must be 1..%d", MaxEmbeddingDimension)
	case d.Form < 1 || d.Form > MaxEmbeddingForm:
		return fmt.Errorf("form must be 1..%d", MaxEmbeddingForm)
	case !(d.Floor > 0 && d.Floor <= 1):
		return fmt.Errorf("floor must be above 0 and at most 1")
	case !reModelName.MatchString(d.Model):
		return fmt.Errorf("model must name an entry of %s", ModelsFile)
	case !(d.ProbeBound >= MinProbeBound && d.ProbeBound <= 1):
		return fmt.Errorf("probe_bound must be %.1f..1", MinProbeBound)
	case len(d.Probes) < 1 || len(d.Probes) > MaxEmbeddingProbes:
		return fmt.Errorf("probes must hold 1..%d entries", MaxEmbeddingProbes)
	}
	for i, p := range d.Probes {
		if p.Kind != EmbedKindMemory && p.Kind != EmbedKindCue {
			return fmt.Errorf("probe %d: kind must be %q or %q", i+1, EmbedKindMemory, EmbedKindCue)
		}
		if n := utf8.RuneCountInString(p.Text); n < 1 || n > MaxEmbedChars || !utf8.ValidString(p.Text) {
			return fmt.Errorf("probe %d: text must be 1..%d characters of valid UTF-8", i+1, MaxEmbedChars)
		}

		if strings.TrimSpace(p.Text) == "" {
			return fmt.Errorf("probe %d: text is white space only, which a source is never handed", i+1)
		}
		if len(p.Vector) != d.Dimension {
			return fmt.Errorf("probe %d: vector has %d numbers, the declared dimension is %d", i+1, len(p.Vector), d.Dimension)
		}
		var norm float64
		for _, v := range p.Vector {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("probe %d: vector holds a number that is not finite", i+1)
			}
			norm += v * v
		}
		if norm == 0 {
			return fmt.Errorf("probe %d: vector is all zeros, which no cosine can be taken against", i+1)
		}

		if math.IsInf(norm, 0) {
			return fmt.Errorf("probe %d: vector's entries are too large for its length to be a number, so no cosine can be taken against it", i+1)
		}
	}

	kinds := map[string]bool{}
	for _, p := range d.Probes {
		kinds[p.Kind] = true
	}
	if !kinds[EmbedKindMemory] || !kinds[EmbedKindCue] {
		return fmt.Errorf("probes must hold at least one %s and one %s", EmbedKindMemory, EmbedKindCue)
	}
	return nil
}

func (c *AuthorConfig) declaresEmbeddings() (AuthorInterface, bool) {
	for _, iface := range c.InterfaceList() {
		if iface.ID == EmbeddingsInterfaceID {
			return iface, true
		}
	}
	return AuthorInterface{}, false
}

func (c *AuthorConfig) validateEmbeddingsSource() error {
	iface, declared := c.declaresEmbeddings()
	if !declared {
		if c.EmbeddingsFile != "" {
			return fmt.Errorf("embeddings_file is set and no %s interface is declared", EmbeddingsInterfaceID)
		}
		return nil
	}
	refuse := func(format string, args ...any) error {
		return fmt.Errorf("a source of vectors (%s): %s", EmbeddingsInterfaceID, fmt.Sprintf(format, args...))
	}
	switch {
	case c.PluginFamily != EmbeddingsFamily:
		return refuse("plugin_family must be %s, not %s", EmbeddingsFamily, c.PluginFamily)
	case iface.Version != EmbeddingsInterfaceVersion:
		return refuse("the interface's version is %d; this kit writes @%d", iface.Version, EmbeddingsInterfaceVersion)
	case len(c.InterfaceList()) != 1:
		return refuse("it is the package's only interface")
	case !ValidHostVersion(c.AiiosMinVersion) || CompareHostVersion(c.AiiosMinVersion, EmbeddingsMinHost) < 0:
		return refuse("requires aiios_min_version >= %s; an earlier host would offer embed to a resident as a tool", EmbeddingsMinHost)
	case len(c.CapabilityEnvelope) != 0:
		return refuse("capability_envelope must be []; it names %q", c.CapabilityEnvelope[0])
	case len(c.Settings) != 0:
		return refuse("it declares no settings")
	case len(c.Webhooks) != 0:
		return refuse("it declares no webhooks")
	case len(c.Schedule) != 0:
		return refuse("it declares no schedule")
	case len(c.Subscriptions) != 0:
		return refuse("it declares no subscriptions")
	case c.EmbeddingsFile == "":
		return refuse("embeddings_file must name its declaration")
	}
	for _, v := range c.Variants {

		if v.ExecutionRuntime != "native_t3_component" {
			return refuse("variant %s must be native: only a native child is handed the model its vectors are named by", v.VariantID)
		}
		if len(v.VariantCapabilities) != 0 {
			return refuse("variant %s: variant_capabilities must be []; it names %q", v.VariantID, v.VariantCapabilities[0])
		}
	}
	return nil
}

func ValidateEmbeddingsPackage(c *AuthorConfig, d *EmbeddingsDecl, methods []string) error {
	if _, declared := c.declaresEmbeddings(); !declared {
		return fmt.Errorf("the package declares no %s interface", EmbeddingsInterfaceID)
	}
	if len(methods) != 1 || methods[0] != MethodEmbed {
		return fmt.Errorf("a source of vectors describes one operation, %s; this plugin describes %v", MethodEmbed, methods)
	}
	for _, m := range c.Models {
		if m.Name != d.Model {
			continue
		}
		if m.When != nil {
			return fmt.Errorf("%s names model %q, which is needed only for some values of a setting; a source's model is always present", EmbeddingsFile, d.Model)
		}
		return nil
	}
	return fmt.Errorf("%s names model %q, which models does not declare", EmbeddingsFile, d.Model)
}

func ValidateEmbeddingsDescriptors(descriptors []byte) error {
	var list []struct {
		ID               string `json:"id"`
		OperatorConfirms bool   `json:"operator_confirms"`
	}
	if err := json.Unmarshal(descriptors, &list); err != nil {
		return fmt.Errorf("descriptor emission is not a JSON array: %w", err)
	}
	for _, d := range list {
		if d.OperatorConfirms {
			return fmt.Errorf("a source of vectors' %s may not declare operator_confirms: the host drives it, and refuses a package whose descriptor asks", d.ID)
		}
	}
	return nil
}
