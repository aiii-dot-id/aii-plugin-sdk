# twilio-sms

An SMS channel adapter built with the kit: the three methods every
adapter speaks — describe, send, receive — and one webhook operation
Twilio calls when a message arrives.

    aiisdk build && aiisdk test

The cases prove the adapter without the network: describe names the
channel and says it receives by webhook; an inbound Twilio form becomes
an arrival and a TwiML reply; a body that is not a message is refused;
a send is denied without a grant; receive says it is push.

On an identity: install at T2 (the credential floor), grant
`net.outbound:api.twilio.com:443`, kv (the send window's) and a credential handle in
`plugins.grants.<id>`, configure that handle as an auth profile with
`"scheme": "basic"` pinned to `api.twilio.com:443` whose secret is
`AccountSid:AuthToken`, set `account_sid`, `from_number` and the
`auth_token` handle in the plugins view, and point Twilio's message
webhook at `https://<public name>/hooks/com.aiii.examples.twilio-sms/sms`.
The host verifies Twilio's signature with the token — this code never
sees it — records each arrival once by MessageSid, and wakes the
identity only for a contact the operator granted wake.

**Send window.** A channel's sends are bounded: `send_limit` in any
`send_window_minutes` (defaults 20 and 60, both settings on the plugin's card,
1 to 1000 and 1 to 1440). A send over the limit is refused before Twilio is
asked, in `NET_REMOTE_FAILED` — nothing accepted — saying which limit, where the
operator changes it, and when the next send may go. A send whose outcome is not
known counts, for it may have gone; one Twilio refused does not. The times of the
recent sends are the plugin's own kv (`sdk.SendWindow`), so the adapter needs
`ring4.kv`; without the grant, or without the host's clock, it sends nothing and
says which is missing. This is the field's containment for a runaway identity
(a per-window rate limit on autonomous sends): a contact in the address book is
the consent to a message, not to a thousand.
