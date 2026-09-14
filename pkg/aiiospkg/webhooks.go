package aiiospkg

// .
// .
// .
// .
// .
// .

import (
	"fmt"
	"regexp"
)

// .
const WebhooksFile = "webhooks.json"

const (
	MaxWebhooks     = 16
	MaxWebhookBytes = 256 << 10
)

// .
const (
	SchemeHMACSHA256Hex    = "hmac-sha256-hex"
	SchemeHMACSHA256Base64 = "hmac-sha256-base64"
	SchemeTwilio           = "twilio"
	SchemeToken            = "token"
)

var (
	reWebhookPath = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}(/[a-z0-9][a-z0-9_-]{0,63}){0,3}$`)
	reHeaderName  = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
)

// .
type WebhookSignature struct {
	Scheme        string `json:"scheme"`
	Header        string `json:"header"`
	Prefix        string `json:"prefix,omitempty"`
	SecretSetting string `json:"secret_setting"`
}

// .
type WebhookDecl struct {
	Path         string            `json:"path"`
	Operation    string            `json:"operation"`
	Signature    *WebhookSignature `json:"signature"`
	MaxBodyBytes int               `json:"max_body_bytes,omitempty"`
}

// .
// .
// .
// .
// .
func ValidateWebhooks(decls []WebhookDecl, settings []SettingDecl) error {
	if len(decls) > MaxWebhooks {
		return fmt.Errorf("%d webhooks; at most %d", len(decls), MaxWebhooks)
	}
	secrets := map[string]bool{}
	for _, s := range settings {
		if s.Type == SettingSecret {
			secrets[s.Key] = true
		}
	}
	paths := map[string]bool{}
	for i, d := range decls {
		if !reWebhookPath.MatchString(d.Path) {
			return fmt.Errorf("webhook %d: path %q is not lowercase segments (at most four, 64 bytes each)", i, d.Path)
		}
		if paths[d.Path] {
			return fmt.Errorf("webhook path %q declared twice", d.Path)
		}
		paths[d.Path] = true
		if d.Operation == "" {
			return fmt.Errorf("webhook %q: operation is required", d.Path)
		}
		if d.Signature == nil {
			return fmt.Errorf("webhook %q: a signature is required; the public origin is public", d.Path)
		}
		switch d.Signature.Scheme {
		case SchemeHMACSHA256Hex, SchemeHMACSHA256Base64, SchemeTwilio, SchemeToken:
		default:
			return fmt.Errorf("webhook %q: scheme %q is not hmac-sha256-hex, hmac-sha256-base64, twilio or token", d.Path, d.Signature.Scheme)
		}
		if !reHeaderName.MatchString(d.Signature.Header) {
			return fmt.Errorf("webhook %q: header %q is not a header name", d.Path, d.Signature.Header)
		}
		if len(d.Signature.Prefix) > 32 {
			return fmt.Errorf("webhook %q: prefix over 32 bytes", d.Path)
		}
		if !secrets[d.Signature.SecretSetting] {
			return fmt.Errorf("webhook %q: secret_setting %q is not a secret-typed setting in plugin.json", d.Path, d.Signature.SecretSetting)
		}
		if d.MaxBodyBytes < 0 || d.MaxBodyBytes > MaxWebhookBytes {
			return fmt.Errorf("webhook %q: max_body_bytes must be 0..%d", d.Path, MaxWebhookBytes)
		}
	}
	return nil
}

// .
// .
func CheckWebhookOperations(decls []WebhookDecl, operations []string) error {
	known := map[string]bool{}
	for _, op := range operations {
		known[op] = true
	}
	for _, d := range decls {
		if !known[d.Operation] {
			return fmt.Errorf("webhook %q: operation %q is not one this plugin describes", d.Path, d.Operation)
		}
	}
	return nil
}

// .
func WebhooksJSON(decls []WebhookDecl, settings []SettingDecl) ([]byte, error) {
	if err := ValidateWebhooks(decls, settings); err != nil {
		return nil, err
	}
	list := make([]interface{}, 0, len(decls))
	for _, d := range decls {
		sig := map[string]interface{}{"scheme": d.Signature.Scheme, "header": d.Signature.Header, "secret_setting": d.Signature.SecretSetting}
		if d.Signature.Prefix != "" {
			sig["prefix"] = d.Signature.Prefix
		}
		entry := map[string]interface{}{"path": d.Path, "operation": d.Operation, "signature": sig}
		if d.MaxBodyBytes > 0 {
			entry["max_body_bytes"] = d.MaxBodyBytes
		}
		list = append(list, entry)
	}
	return marshalCanonical(list)
}
