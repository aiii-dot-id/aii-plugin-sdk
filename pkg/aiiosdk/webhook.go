package aiiosdk

import "strings"

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
type WebhookRequest struct {
	Method  string
	Path    string
	Query   string
	Body    string
	headers Object
}

// .
func ParseWebhook(c Call) WebhookRequest {
	args := c.Args()
	var r WebhookRequest
	r.Method, _ = args.String("method")
	r.Path, _ = args.String("path")
	r.Query, _ = args.String("query")
	r.Body, _ = args.String("body")
	r.headers = args.Object("headers")
	return r
}

// .
// .
func (r WebhookRequest) Header(name string) string {
	if r.headers == nil {
		return ""
	}
	v, _ := r.headers.String(name)
	return v
}

// .
// .
func (r WebhookRequest) Form() map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(r.Body, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		k, v = unescapeForm(k), unescapeForm(v)
		if _, seen := out[k]; !seen {
			out[k] = v
		}
	}
	return out
}

func unescapeForm(s string) string {
	s = strings.ReplaceAll(s, "+", " ")
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi, lo := hexNibble(s[i+1]), hexNibble(s[i+2])
			if hi >= 0 && lo >= 0 {
				b.WriteByte(byte(hi<<4 | lo))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func hexNibble(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// .
// .
// .
type Arrival struct {
	ID   string
	From string
	Body string
}

// .
type WebhookResponse struct {
	Status      int
	ContentType string
	Body        string
}

// .
type WebhookResult struct {
	Arrival  *Arrival
	Response *WebhookResponse
}

// .
func (r WebhookResult) Value() map[string]any {
	out := map[string]any{}
	if r.Arrival != nil {
		out["arrival"] = map[string]any{"id": r.Arrival.ID, "from": r.Arrival.From, "body": r.Arrival.Body}
	}
	if r.Response != nil {
		resp := map[string]any{"status": r.Response.Status}
		if r.Response.ContentType != "" {
			resp["content_type"] = r.Response.ContentType
		}
		if r.Response.Body != "" {
			resp["body"] = r.Response.Body
		}
		out["response"] = resp
	}
	return out
}
