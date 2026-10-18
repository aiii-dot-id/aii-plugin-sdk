package aiiospkg

import (
	"strings"
	"testing"
)

func TestWebhookDeclarationsAreHeldToTheRules(t *testing.T) {
	settings := []SettingDecl{{Key: "auth_token", Type: SettingSecret, Title: "Auth token"}, {Key: "repo", Type: SettingString, Title: "Repo"}}
	good := []WebhookDecl{{Path: "sms", Operation: "sms.inbound", Signature: &WebhookSignature{Scheme: SchemeTwilio, Header: "X-Twilio-Signature", SecretSetting: "auth_token"}, MaxBodyBytes: 65536}}
	if err := ValidateWebhooks(good, settings); err != nil {
		t.Fatal(err)
	}
	out, err := WebhooksJSON(good, settings)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"max_body_bytes":65536,"operation":"sms.inbound","path":"sms","signature":{"header":"X-Twilio-Signature","scheme":"twilio","secret_setting":"auth_token"}}]`
	if string(out) != want {
		t.Fatalf("canonical member:\n%s\nwant\n%s", out, want)
	}
	if err := CheckWebhookOperations(good, []string{"send", "receive", "describe"}); err == nil || !strings.Contains(err.Error(), "not one this plugin describes") {
		t.Fatalf("an undescribed operation: %v", err)
	}
	if err := CheckWebhookOperations(good, []string{"sms.inbound"}); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		decl []WebhookDecl
		want string
	}{
		"no signature":  {[]WebhookDecl{{Path: "a", Operation: "x"}}, "signature is required"},
		"bad scheme":    {[]WebhookDecl{{Path: "a", Operation: "x", Signature: &WebhookSignature{Scheme: "md5", Header: "X", SecretSetting: "auth_token"}}}, "scheme"},
		"not a secret":  {[]WebhookDecl{{Path: "a", Operation: "x", Signature: &WebhookSignature{Scheme: "token", Header: "X", SecretSetting: "repo"}}}, "secret-typed"},
		"bad path":      {[]WebhookDecl{{Path: "A/../b", Operation: "x", Signature: &WebhookSignature{Scheme: "token", Header: "X", SecretSetting: "auth_token"}}}, "path"},
		"twice":         {[]WebhookDecl{{Path: "a", Operation: "x", Signature: &WebhookSignature{Scheme: "token", Header: "X", SecretSetting: "auth_token"}}, {Path: "a", Operation: "y", Signature: &WebhookSignature{Scheme: "token", Header: "X", SecretSetting: "auth_token"}}}, "twice"},
		"body over cap": {[]WebhookDecl{{Path: "a", Operation: "x", MaxBodyBytes: 1 << 30, Signature: &WebhookSignature{Scheme: "token", Header: "X", SecretSetting: "auth_token"}}}, "max_body_bytes"},
	} {
		if err := ValidateWebhooks(tc.decl, settings); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v (want %q)", name, err, tc.want)
		}
	}
}

// google-oidc has no shared secret: Authorization: Bearer, verified for the
// service account a string-typed setting names — and no other scheme may name
// one. The host holds the same rules.
func TestAGoogleOIDCWebhookIsDeclaredWithItsAccountAndNoSecret(t *testing.T) {
	settings := []SettingDecl{{Key: "auth_token", Type: SettingSecret, Title: "Token"}, {Key: "push_account", Type: SettingString, Title: "Service account"}}
	oidc := func(change func(*WebhookSignature)) []WebhookDecl {
		sig := &WebhookSignature{Scheme: SchemeGoogleOIDC, Header: "Authorization", Prefix: "Bearer ", EmailSetting: "push_account"}
		if change != nil {
			change(sig)
		}
		return []WebhookDecl{{Path: "notify", Operation: "mail.notify", Signature: sig}}
	}
	if err := ValidateWebhooks(oidc(nil), settings); err != nil {
		t.Fatal(err)
	}
	out, err := WebhooksJSON(oidc(nil), settings)
	if err != nil {
		t.Fatal(err)
	}
	if want := `[{"operation":"mail.notify","path":"notify","signature":{"email_setting":"push_account","header":"Authorization","prefix":"Bearer ","scheme":"google-oidc"}}]`; string(out) != want {
		t.Fatalf("canonical member:\n%s\nwant\n%s", out, want)
	}
	for name, tc := range map[string]struct {
		change func(*WebhookSignature)
		want   string
	}{
		"another header":              {func(s *WebhookSignature) { s.Header = "X-Token" }, "Authorization: Bearer"},
		"no Bearer prefix":            {func(s *WebhookSignature) { s.Prefix = "" }, "Authorization: Bearer"},
		"a secret named":              {func(s *WebhookSignature) { s.SecretSetting = "auth_token" }, "no shared secret"},
		"no account":                  {func(s *WebhookSignature) { s.EmailSetting = "" }, "email_setting"},
		"an account that is a secret": {func(s *WebhookSignature) { s.EmailSetting = "auth_token" }, "string-typed"},
	} {
		if err := ValidateWebhooks(oidc(tc.change), settings); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v (want %q)", name, err, tc.want)
		}
	}
	token := []WebhookDecl{{Path: "a", Operation: "x", Signature: &WebhookSignature{Scheme: SchemeToken, Header: "X", SecretSetting: "auth_token", EmailSetting: "push_account"}}}
	if err := ValidateWebhooks(token, settings); err == nil || !strings.Contains(err.Error(), "google-oidc only") {
		t.Errorf("another scheme naming an account: %v", err)
	}
}
