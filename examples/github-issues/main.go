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

const (
	api  = "https://api.github.com"
	host = "net.outbound:api.github.com:443"
)

func init() {
	p := sdk.New("com.aiii.examples.github-issues")

	p.Describe("issues.list", sdk.Descriptor{
		Summary:      "List the repository's open issues",
		Input:        "schemas/list_in.json",
		Output:       "schemas/list_out.json",
		Effects:      sdk.EffectsReadExternal,
		Capabilities: []string{host},
		Family:       "github",
		Keywords:     []string{"issues", "repository", "github", "open"},
		Examples:     []string{`{"limit": 5}`},
	})
	p.Handle("issues.list", list)

	p.Describe("issues.create", sdk.Descriptor{
		Summary:      "Open an issue in the repository",
		Input:        "schemas/create_in.json",
		Output:       "schemas/create_out.json",
		Effects:      sdk.EffectsWriteExternal,
		Capabilities: []string{host},
		Family:       "github",
		Keywords:     []string{"issue", "create", "open", "file a bug"},
		Examples:     []string{`{"title": "The build breaks on macOS", "body": "Steps: …"}`},
	})
	p.Handle("issues.create", create)

	p.Describe("issues.stream", sdk.Descriptor{
		Summary:      "Consume the issues list as a streamed reply and report its size",
		Input:        "schemas/stream_in.json",
		Output:       "schemas/stream_out.json",
		Effects:      sdk.EffectsReadExternal,
		Capabilities: []string{host},
		Family:       "github",
	})
	p.Handle("issues.stream", stream)

	p.Run()
}

// .
// .
// .
func settings() (repo, token string, err error) {
	vals, err := sdk.Settings.Load()
	if err != nil {
		return "", "", err
	}
	repo, ok := vals.String("repo")
	if !ok || repo == "" {
		return "", "", sdk.Fail("OPERATION_ARGUMENT_INVALID", "the repo setting (owner/name) is not set — set it in the plugins view")
	}
	token, _ = vals.Handle("token")
	return repo, token, nil
}

func options(token string) *sdk.HTTPOptions {
	return &sdk.HTTPOptions{Headers: map[string]string{"Accept": "application/vnd.github+json"}, AuthProfile: token}
}

func list(c sdk.Call) (any, error) {
	repo, token, err := settings()
	if err != nil {
		return nil, err
	}
	limit := int64(10)
	if n, ok := c.Args().Int("limit"); ok && n >= 1 && n <= 50 {
		limit = n
	}
	res, err := sdk.HTTP.Get(fmt.Sprintf("%s/repos/%s/issues?state=open&per_page=%d", api, repo, limit), options(token))
	if err != nil {
		return failure(err, res)
	}
	return map[string]any{"http_status": res.Status, "issues": res.Body, "effect": res.Effect}, nil
}

func create(c sdk.Call) (any, error) {
	repo, token, err := settings()
	if err != nil {
		return nil, err
	}
	title, ok := c.Args().String("title")
	if !ok || title == "" {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "issues.create requires arguments.title")
	}
	body, _ := c.Args().String("body")
	payload, _ := json.Marshal(map[string]string{"title": title, "body": body})
	opts := options(token)
	opts.Body, opts.ContentType = string(payload), "application/json"
	// .
	// .
	res, err := sdk.HTTP.Post(fmt.Sprintf("%s/repos/%s/issues", api, repo), opts)
	if err != nil {
		if sdk.EffectUnknown(err) {
			return nil, sdk.Fail("NET_EFFECT_UNKNOWN", "the request was sent and the response was lost — the issue may exist; list before retrying")
		}
		return failure(err, res)
	}
	out := sdk.Object(res.Body)
	number, _ := out.Int("number")
	url, _ := out.String("html_url")
	return map[string]any{"number": number, "html_url": url, "effect": res.Effect}, nil
}

func stream(c sdk.Call) (any, error) {
	repo, token, err := settings()
	if err != nil {
		return nil, err
	}
	limit := int64(10)
	if n, ok := c.Args().Int("limit"); ok && n >= 1 && n <= 50 {
		limit = n
	}
	s, err := sdk.HTTP.Stream("GET", fmt.Sprintf("%s/repos/%s/issues?state=open&per_page=%d", api, repo, limit), options(token))
	if err != nil {
		return failure(err, sdk.HTTPResult{})
	}
	data, err := s.ReadAll(1 << 20)
	if err != nil {
		return failure(err, sdk.HTTPResult{})
	}
	return map[string]any{"http_status": s.Status, "bytes": len(data), "chunks": s.Chunks, "content_type": s.ContentType}, nil
}

// .
// .
// .
func failure(err error, res sdk.HTTPResult) (any, error) {
	if d, ok := sdk.AsDenied(err); ok {
		return nil, sdk.Deny(d.ReasonCode, "the host denied "+d.Message+" — grant "+host+" in plugins.grants hosts, and a credential handle for writes")
	}
	if res.Status >= 400 {
		return nil, sdk.Fail("NET_REMOTE_OUTCOME_FAILED", fmt.Sprintf("GitHub answered %d", res.Status))
	}
	return nil, err
}

// .
// .
func main() { sdk.MainDescribe() }
