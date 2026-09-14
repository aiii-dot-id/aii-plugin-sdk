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
package main

import (
	"encoding/json"
	"fmt"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

const host = "net.outbound:mcp.example.test:443"

func init() {
	p := sdk.New("com.aiii.examples.mcp-connector")

	p.Describe("mcp.refresh", sdk.Descriptor{
		Summary:      "Ask the server for its tools and publish them",
		Input:        "schemas/refresh_in.json",
		Output:       "schemas/publish_out.json",
		Effects:      sdk.EffectsWriteExternal,
		Capabilities: []string{"tools.publish", host},
		Family:       "mcp",
		Keywords:     []string{"mcp", "server", "tools", "refresh", "connect"},
	})
	p.Handle("mcp.refresh", refresh)

	p.Describe("mcp.publish_from", sdk.Descriptor{
		Summary:      "Publish tools from a given list (the server's tools/list result)",
		Input:        "schemas/publish_from_in.json",
		Output:       "schemas/publish_out.json",
		Effects:      sdk.EffectsWriteLocal,
		Capabilities: []string{"tools.publish"},
		Family:       "mcp",
	})
	p.Handle("mcp.publish_from", publishFrom)

	// .
	// .
	p.HandleDynamic(forward)

	p.Run()
}

func serverOptions() (url string, opts *sdk.HTTPOptions, err error) {
	vals, err := sdk.Settings.Load()
	if err != nil {
		return "", nil, err
	}
	url, ok := vals.String("server_url")
	if !ok || url == "" {
		return "", nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "the server_url setting is not set — set it in the plugins view")
	}
	token, _ := vals.Handle("token")
	return url, &sdk.HTTPOptions{ContentType: "application/json", AuthProfile: token, Headers: map[string]string{"Accept": "application/json"}}, nil
}

func rpc(method string, params any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	return string(b)
}

func refresh(c sdk.Call) (any, error) {
	url, opts, err := serverOptions()
	if err != nil {
		return nil, err
	}
	opts.Body = rpc("tools/list", map[string]any{})
	res, err := sdk.HTTP.Post(url, opts)
	if err != nil {
		return failure(err)
	}
	result := sdk.Object(res.Body).Object("result")
	if result == nil {
		return nil, sdk.Fail("NET_REMOTE_OUTCOME_FAILED", "the server answered tools/list without a result")
	}
	return publishList(result.Raw("tools"))
}

func publishFrom(c sdk.Call) (any, error) {
	return publishList(c.Args().Raw("tools"))
}

// .
// .
// .
func publishList(raw []byte) (any, error) {
	items, ok := sdk.ObjectArray(raw)
	if !ok {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "tools must be a list of tool objects")
	}
	var published, refused []any
	for _, item := range items {
		name, _ := item.String("name")
		desc, _ := item.String("description")
		if name == "" {
			continue
		}
		if desc == "" {
			desc = "MCP tool " + name
		}
		spec := sdk.PublishSpec{Name: "mcp." + name, Summary: desc, Effects: sdk.EffectsWriteExternal, Capabilities: []string{host}, Family: "mcp"}
		if schema := item.Raw("inputSchema"); len(schema) > 0 {
			spec.Input = json.RawMessage(schema)
		}
		registry, err := sdk.Tools.Publish(spec)
		if err != nil {
			if d, ok := sdk.AsDenied(err); ok {
				return nil, sdk.Deny(d.ReasonCode, "the host denied "+d.Message+" — grant tools (plugins.grants.<id>.tools) to let this connector publish")
			}
			refused = append(refused, map[string]any{"name": name, "reason": err.Error()})
			continue
		}
		published = append(published, map[string]any{"name": "mcp." + name, "tool": registry})
	}
	return map[string]any{"published": published, "refused": refused}, nil
}

func forward(name string, c sdk.Call) (any, error) {
	url, opts, err := serverOptions()
	if err != nil {
		return nil, err
	}
	var args any = json.RawMessage(c.Arguments)
	if len(c.Arguments) == 0 {
		args = map[string]any{}
	}
	tool := name
	if len(tool) > 4 && tool[:4] == "mcp." {
		tool = tool[4:]
	}
	opts.Body = rpc("tools/call", map[string]any{"name": tool, "arguments": args})
	res, err := sdk.HTTP.Post(url, opts)
	if err != nil {
		return failure(err)
	}
	return map[string]any{"http_status": res.Status, "result": res.Body, "effect": res.Effect}, nil
}

func failure(err error) (any, error) {
	if d, ok := sdk.AsDenied(err); ok {
		return nil, sdk.Deny(d.ReasonCode, "the host denied "+d.Message+" — grant "+host+" and a credential handle in plugins.grants")
	}
	if sdk.EffectUnknown(err) {
		return nil, sdk.Fail("NET_EFFECT_UNKNOWN", "the call was sent and the response was lost")
	}
	return nil, err
}

// .
// .
func main() { sdk.MainDescribe() }

var _ = fmt.Sprintf
