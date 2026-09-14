package aiiospkg

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

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

// .
const AuthorFileName = "plugin.json"

var (
	reManifestID  = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*(\.[a-z0-9]+([._-][a-z0-9]+)*)*$`)
	reVariantID   = regexp.MustCompile(`^[a-z0-9._-]+$`)
	reInterfaceID = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
	reCapability  = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+(:.+)?$`)
	rePredicate   = regexp.MustCompile(`^(facility|runtime|distribution|backend|permission|topology):[a-z0-9._/-]+$`)
)

// .
type AuthorConfig struct {
	ID           string `json:"id"`
	Version      string `json:"version"`
	PluginFamily string `json:"plugin_family"`

	// .
	Publisher   string `json:"publisher,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	License     string `json:"license,omitempty"`
	Homepage    string `json:"homepage,omitempty"`

	// .
	Interface AuthorInterface `json:"interface"`
	// .
	// .
	// .
	// .
	// .
	// .
	// .
	// .
	// .
	Interfaces         []AuthorInterface   `json:"interfaces,omitempty"`
	CapabilityEnvelope []string            `json:"capability_envelope"`
	Requirements       *AuthorRequirements `json:"requirements,omitempty"`
	DefaultVariant     string              `json:"default_variant,omitempty"`
	Variants           []AuthorVariant     `json:"variants"`

	// .
	// .
	// .
	// .
	// .
	// .
	Settings []SettingDecl `json:"settings,omitempty"`

	// .
	// .
	// .
	Webhooks []WebhookDecl `json:"webhooks,omitempty"`

	// .
	// .
	// .
	Subscriptions []SubscriptionDecl `json:"subscriptions,omitempty"`

	// .
	// .
	Models []ModelDecl `json:"models,omitempty"`

	// .
	// .
	// .
	// .
	Runtimes []RuntimeDecl `json:"runtimes,omitempty"`
}

// .
// .
// .
// .
type AuthorInterface struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	// .
	// .
	// .
	// .
	// .
	// .
	// .
	Methods []string `json:"methods,omitempty"`
}

// .
type AuthorRequirements struct {
	Required []string `json:"required"`
	Optional []string `json:"optional"`
}

// .
// .
// .
type AuthorVariant struct {
	VariantID           string              `json:"variant_id"`
	Platform            string              `json:"platform"`
	Arch                string              `json:"arch"`
	Topology            string              `json:"topology"`
	ExecutionRuntime    string              `json:"execution_runtime"`
	AdmissionProfile    string              `json:"admission_profile"`
	VariantCapabilities []string            `json:"variant_capabilities"`
	Requirements        *AuthorRequirements `json:"requirements,omitempty"`

	// .
	// .
	// .
	// .
	// .
	Accelerator *AcceleratorProfile `json:"accelerator,omitempty"`

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
	Artifact string `json:"artifact,omitempty"`
}

// .
// .
// .
// .
func (v *AuthorVariant) RequiresOwnArtifact() bool {
	switch v.ExecutionRuntime {
	case "wasm_component", "wasm_aot_component":
		return false
	}
	return true
}

// .
// .
// .
func (v *AuthorVariant) isT3() bool {
	return v.AdmissionProfile == "platform_reserved" ||
		(v.ExecutionRuntime == "inprocess_component" && v.AdmissionProfile == "certified_native")
}

// .
// .
func (c *AuthorConfig) Root() string { return c.ID + "-" + c.Version }

// .
func ValidPluginID(id string) bool { return reManifestID.MatchString(id) }

// .
func LoadAuthorConfig(path string) (*AuthorConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var cfg AuthorConfig
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("%s: %w (the authoring file is closed — check member spelling)", path, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%s: trailing data after the plugin.json object", path)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &cfg, nil
}

func enumHas(value string, allowed ...string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}

// .
// .
func (c *AuthorConfig) Validate() error {
	if !reManifestID.MatchString(c.ID) {
		return fmt.Errorf("id %q does not match the manifest id grammar (lowercase dotted, e.g. com.example.hello)", c.ID)
	}
	if c.Version == "" {
		return fmt.Errorf("version is required")
	}
	if !enumHas(c.PluginFamily, "channel_adapter", "provider_bridge", "tool_bridge", "voice_interface") {
		return fmt.Errorf("plugin_family %q must be one of channel_adapter, provider_bridge, tool_bridge, voice_interface", c.PluginFamily)
	}
	if len(c.Interfaces) > 0 && c.Interface.ID != "" {
		return fmt.Errorf("declare either interface or interfaces, never both — two answers to which interface this is would make the manifest ambiguous")
	}
	if len(c.Interfaces) == 1 {
		return fmt.Errorf("interfaces declares one entry; a package with a single interface uses interface")
	}
	seenIface := map[string]bool{}
	for _, iface := range c.InterfaceList() {
		if !reInterfaceID.MatchString(iface.ID) {
			return fmt.Errorf("interface.id %q does not match the interface grammar (lowercase dotted with at least one dot)", iface.ID)
		}
		if iface.Version < 1 {
			return fmt.Errorf("interface %s: version must be >= 1, got %d", iface.ID, iface.Version)
		}
		if seenIface[iface.ID] {
			return fmt.Errorf("interface %s is declared twice", iface.ID)
		}
		seenIface[iface.ID] = true
	}
	if c.CapabilityEnvelope == nil {
		return fmt.Errorf("capability_envelope is required (use [] for the zero-capability posture)")
	}
	if err := validateCapabilityList(c.CapabilityEnvelope, "capability_envelope"); err != nil {
		return err
	}
	if err := validateAuthorRequirements(c.Requirements, "requirements"); err != nil {
		return err
	}
	if len(c.Variants) == 0 {
		return fmt.Errorf("at least one variant is required (T0-T2 releases need a WASM baseline variant)")
	}
	seen := map[string]bool{}
	hasWASM := false
	for i := range c.Variants {
		v := &c.Variants[i]
		if err := v.validate(); err != nil {
			return err
		}
		if seen[v.VariantID] {
			return fmt.Errorf("duplicate variant_id %q", v.VariantID)
		}
		seen[v.VariantID] = true
		if v.ExecutionRuntime == "wasm_component" || v.ExecutionRuntime == "wasm_aot_component" {
			hasWASM = true
		}
	}
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
	needsBaseline := false
	for i := range c.Variants {
		if !c.Variants[i].isT3() {
			needsBaseline = true
			break
		}
	}
	if needsBaseline && !hasWASM {
		return fmt.Errorf("no WASM variant declared — the tier contract requires a WASM baseline for T0/T1/T2 (PLUGIN_BUNDLE_FORMAT.md §1); a package whose variants are all platform_reserved or mobile in-process is exempt")
	}
	if c.DefaultVariant != "" && !seen[c.DefaultVariant] {
		return fmt.Errorf("default_variant %q is not a declared variant", c.DefaultVariant)
	}
	if err := ValidateSettings(c.Settings); err != nil {
		return fmt.Errorf("settings: %v", err)
	}
	if err := ValidateWebhooks(c.Webhooks, c.Settings); err != nil {
		return fmt.Errorf("webhooks: %v", err)
	}
	if err := ValidateSubscriptions(c.Subscriptions); err != nil {
		return fmt.Errorf("subscriptions: %v", err)
	}
	if err := ValidateModels(c.Models); err != nil {
		return fmt.Errorf("models: %v", err)
	}
	knownModels := map[string]bool{}
	for _, m := range c.Models {
		knownModels[m.Name] = true
	}
	for i := range c.Variants {
		v := &c.Variants[i]
		if v.Accelerator == nil {
			continue
		}
		if v.ExecutionRuntime != "native_t3_component" {
			return fmt.Errorf("variant %s: only a native_t3_component variant declares an accelerator profile", v.VariantID)
		}
		if err := ValidateAcceleratorProfile(v.VariantID, v.Accelerator); err != nil {
			return err
		}
		for _, name := range v.Accelerator.Models {
			if !knownModels[name] {
				return fmt.Errorf("variant %s: accelerator names model %q, which models does not declare", v.VariantID, name)
			}
		}
	}
	return nil
}

func (v *AuthorVariant) validate() error {
	if !reVariantID.MatchString(v.VariantID) {
		return fmt.Errorf("variant_id %q does not match the variant grammar [a-z0-9._-]+", v.VariantID)
	}
	if !enumHas(v.Platform, "linux", "macos", "windows", "android", "ios") {
		return fmt.Errorf("variant %s: platform %q must be one of linux, macos, windows, android, ios", v.VariantID, v.Platform)
	}
	if !enumHas(v.Arch, "x86_64", "arm64") {
		return fmt.Errorf("variant %s: arch %q must be x86_64 or arm64", v.VariantID, v.Arch)
	}
	if !enumHas(v.Topology, "full_identity_host", "mobile_app_host") {
		return fmt.Errorf("variant %s: topology %q must be full_identity_host or mobile_app_host", v.VariantID, v.Topology)
	}
	if !enumHas(v.ExecutionRuntime, "service_process", "wasm_component", "wasm_aot_component", "inprocess_component", "native_t3_component") {
		return fmt.Errorf("variant %s: execution_runtime %q is not a supported runtime", v.VariantID, v.ExecutionRuntime)
	}
	if !enumHas(v.AdmissionProfile, "standard", "wasm_sandbox", "certified_native", "platform_reserved") {
		return fmt.Errorf("variant %s: admission_profile %q is not a supported profile", v.VariantID, v.AdmissionProfile)
	}
	// .
	// .
	pairingOK := false
	switch v.ExecutionRuntime {
	case "service_process":
		pairingOK = enumHas(v.AdmissionProfile, "standard", "certified_native")
	case "wasm_component", "wasm_aot_component":
		pairingOK = v.AdmissionProfile == "wasm_sandbox"
	case "inprocess_component":
		pairingOK = v.AdmissionProfile == "certified_native" &&
			enumHas(v.Platform, "android", "ios") && v.Topology == "mobile_app_host"
	case "native_t3_component":
		pairingOK = v.AdmissionProfile == "platform_reserved"
	}
	if !pairingOK {
		return fmt.Errorf("variant %s pairs runtime %q with profile %q, which the schema forbids (wasm needs wasm_sandbox; service_process needs standard or certified_native)", v.VariantID, v.ExecutionRuntime, v.AdmissionProfile)
	}
	if v.VariantCapabilities == nil {
		return fmt.Errorf("variant %s: variant_capabilities is required (use [] for none)", v.VariantID)
	}
	if err := validateCapabilityList(v.VariantCapabilities, fmt.Sprintf("variant %s variant_capabilities", v.VariantID)); err != nil {
		return err
	}
	return validateAuthorRequirements(v.Requirements, fmt.Sprintf("variant %s requirements", v.VariantID))
}

func validateCapabilityList(caps []string, field string) error {
	if len(caps) > 32 {
		return fmt.Errorf("%s declares more than 32 entries", field)
	}
	seen := map[string]bool{}
	for _, c := range caps {
		if !reCapability.MatchString(c) {
			return fmt.Errorf("%s entry %q does not match the capability grammar", field, c)
		}
		if seen[c] {
			return fmt.Errorf("%s entry %q duplicated", field, c)
		}
		seen[c] = true
	}
	return nil
}

func validateAuthorRequirements(r *AuthorRequirements, where string) error {
	if r == nil {
		return nil
	}
	for _, list := range []struct {
		label   string
		entries []string
	}{{where + ".required", r.Required}, {where + ".optional", r.Optional}} {
		if len(list.entries) > 16 {
			return fmt.Errorf("%s declares more than 16 entries", list.label)
		}
		seen := map[string]bool{}
		for _, entry := range list.entries {
			if !rePredicate.MatchString(entry) {
				return fmt.Errorf("%s entry %q does not match the requirement grammar class:name with a known class (facility, runtime, distribution, backend, permission, topology)", list.label, entry)
			}
			if seen[entry] {
				return fmt.Errorf("%s entry %q duplicated", list.label, entry)
			}
			seen[entry] = true
		}
	}
	return nil
}
