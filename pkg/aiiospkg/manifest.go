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

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// .
// .
// .
// .
// .
func SchemaFileRel(iface AuthorInterface) string {
	return fmt.Sprintf("interfaces/%s.v%d.schema.json", iface.ID, iface.Version)
}

// .
// .
// .
// .
func (c *AuthorConfig) InterfaceList() []AuthorInterface {
	if len(c.Interfaces) > 0 {
		return c.Interfaces
	}
	return []AuthorInterface{c.Interface}
}

// .
// .
// .
// .
// .
// .
func PartitionMethods(ifaces []AuthorInterface, ids []string) (map[string][]string, error) {
	if len(ifaces) == 0 {
		return nil, fmt.Errorf("no interface declared")
	}
	out := map[string][]string{}
	if len(ifaces) == 1 && len(ifaces[0].Methods) == 0 {
		out[ifaces[0].ID] = append([]string(nil), ids...)
		return out, checkMethodCounts(ifaces, out)
	}
	declared := map[string]string{}
	known := map[string]bool{}
	for _, id := range ids {
		known[id] = true
	}
	for _, iface := range ifaces {
		if len(iface.Methods) == 0 {
			return nil, fmt.Errorf("interface %s declares no methods; with more than one interface each must name its own", iface.ID)
		}
		for _, m := range iface.Methods {
			if !known[m] {
				return nil, fmt.Errorf("interface %s names method %q, which the plugin does not describe (described: %s)", iface.ID, m, strings.Join(ids, ", "))
			}
			if prior, dup := declared[m]; dup {
				return nil, fmt.Errorf("method %q is claimed by both %s and %s; an operation belongs to exactly one interface", m, prior, iface.ID)
			}
			declared[m] = iface.ID
		}
		out[iface.ID] = append([]string(nil), iface.Methods...)
	}
	var orphans []string
	for _, id := range ids {
		if declared[id] == "" {
			orphans = append(orphans, id)
		}
	}
	if len(orphans) > 0 {
		return nil, fmt.Errorf("described operations belong to no declared interface: %s — an operation no interface claims is unreachable", strings.Join(orphans, ", "))
	}
	return out, checkMethodCounts(ifaces, out)
}

// .
// .
// .
func checkMethodCounts(ifaces []AuthorInterface, byIface map[string][]string) error {
	for _, iface := range ifaces {
		n := len(byIface[iface.ID])
		if n == 0 || n > 16 {
			return fmt.Errorf("interface %s would declare %d methods; the manifest grammar admits 1..16 per interface", iface.ID, n)
		}
	}
	if len(ifaces) > 16 {
		return fmt.Errorf("%d interfaces declared; the manifest grammar admits at most 16", len(ifaces))
	}
	return nil
}

// .
// .
// .
// .
// .
// .
func DescriptorsSubset(descriptors []byte, ids []string) ([]byte, error) {
	var rows []json.RawMessage
	if err := json.Unmarshal(descriptors, &rows); err != nil {
		return nil, fmt.Errorf("descriptor emission is not a JSON array: %w", err)
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	out := []byte{'['}
	n := 0
	for _, row := range rows {
		var head struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(row, &head); err != nil {
			return nil, fmt.Errorf("descriptor is not an object: %w", err)
		}
		if !want[head.ID] {
			continue
		}
		if n > 0 {
			out = append(out, ',')
		}
		out = append(out, row...)
		n++
	}
	if n != len(want) {
		return nil, fmt.Errorf("descriptor emission holds %d of the %d named operations", n, len(want))
	}
	return append(out, ']'), nil
}

// .
// .
// .
// .
func EntrypointRel(v AuthorVariant) string {
	return "variants/" + v.VariantID + "/" + entrypointName(v)
}

// .
// .
// .
// .
func entrypointName(v AuthorVariant) string {
	switch v.ExecutionRuntime {
	case "wasm_component", "wasm_aot_component":
		return "plugin.wasm"
	}
	if v.Platform == "windows" {
		return "plugin.exe"
	}
	return "plugin"
}

// .
// .
// .
// .
// .
func DescriptorIDs(descriptors []byte) ([]string, error) {
	var list []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(descriptors, &list); err != nil {
		return nil, fmt.Errorf("descriptor emission is not a JSON array: %w", err)
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("descriptor emission is empty — a plugin manifest requires at least one method; register handlers in init() and re-check `go run .`")
	}
	// .
	// .
	// .
	// .
	if len(list) > 16*16 {
		return nil, fmt.Errorf("plugin declares %d operations; the manifest grammar admits at most 16 interfaces of 16 methods", len(list))
	}
	ids := make([]string, 0, len(list))
	for i, d := range list {
		if d.ID == "" {
			return nil, fmt.Errorf("descriptor %d has no id", i)
		}
		ids = append(ids, d.ID)
	}
	return ids, nil
}

// .
// .
// .
// .
func BuildManifest(cfg *AuthorConfig, methods []string, installFiles map[string][]byte) ([]byte, error) {
	ifaces := cfg.InterfaceList()
	byIface, err := PartitionMethods(ifaces, methods)
	if err != nil {
		return nil, err
	}
	// .
	// .
	// .
	coreDecls := make([]map[string]interface{}, 0, len(ifaces))
	ifaceRefs := make([]string, 0, len(ifaces))
	for _, iface := range ifaces {
		schemaRel := SchemaFileRel(iface)
		schemaBytes, ok := installFiles[schemaRel]
		if !ok {
			return nil, fmt.Errorf("staged install-root is missing the interface schema file %s", schemaRel)
		}
		schemaSum := sha256.Sum256(schemaBytes)
		coreDecls = append(coreDecls, map[string]interface{}{
			"id":          iface.ID,
			"version":     iface.Version,
			"schema_hash": "sha256:" + hex.EncodeToString(schemaSum[:]),
			"methods":     byIface[iface.ID],
		})
		ifaceRefs = append(ifaceRefs, fmt.Sprintf("%s@%d", iface.ID, iface.Version))
	}

	var variants []map[string]interface{}
	for _, v := range cfg.Variants {
		entry := EntrypointRel(v)
		artifact, ok := installFiles[entry]
		if !ok {
			return nil, fmt.Errorf("staged install-root is missing variant %s's entrypoint %s", v.VariantID, entry)
		}
		artifactSum := sha256.Sum256(artifact)
		ventry := map[string]interface{}{
			"variant_id":           v.VariantID,
			"platform":             v.Platform,
			"arch":                 v.Arch,
			"topology":             v.Topology,
			"execution_runtime":    v.ExecutionRuntime,
			"admission_profile":    v.AdmissionProfile,
			"entrypoint":           entry,
			"artifact_hash":        "sha256:" + hex.EncodeToString(artifactSum[:]),
			"implements":           map[string]interface{}{"core": ifaceRefs},
			"variant_capabilities": v.VariantCapabilities,
		}
		if v.Requirements != nil {
			ventry["requirements"] = requirementsValue(v.Requirements)
		}
		variants = append(variants, ventry)
	}

	defaultVariant := cfg.DefaultVariant
	if defaultVariant == "" {
		defaultVariant = cfg.Variants[0].VariantID
	}

	m := map[string]interface{}{
		"kind":                 "plugin",
		"id":                   cfg.ID,
		"version":              cfg.Version,
		"package_hash":         PackageHash(installFiles),
		"plugin_family":        cfg.PluginFamily,
		"bbb_protocol_version": 2,
		"interfaces": map[string]interface{}{
			"core": coreDecls,
		},
		"capability_envelope": cfg.CapabilityEnvelope,
		"default_variant":     defaultVariant,
		"variants":            variants,
	}
	if cfg.Requirements != nil {
		m["requirements"] = requirementsValue(cfg.Requirements)
	}
	for key, value := range map[string]string{
		"aiios_min_version":           cfg.AiiosMinVersion,
		"aiios_max_exclusive_version": cfg.AiiosMaxExclusiveVersion,
		"publisher":                   cfg.Publisher,
		"title":                       cfg.Title,
		"description":                 cfg.Description,
		"license":                     cfg.License,
		"homepage":                    cfg.Homepage,
	} {
		if value != "" {
			m[key] = value
		}
	}
	return marshalCanonical(m)
}

func requirementsValue(r *AuthorRequirements) map[string]interface{} {
	out := map[string]interface{}{}
	if r.Required != nil {
		out["required"] = r.Required
	}
	if r.Optional != nil {
		out["optional"] = r.Optional
	}
	return out
}
