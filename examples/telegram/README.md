# telegram

A Telegram channel adapter built with the kit: the three methods every
adapter speaks — describe, send, receive — with receive a long poll of
the bot API, so it needs no public origin and no webhook.

    aiisdk build && aiisdk test

The cases prove the adapter without the network: describe names the
channel, says it receives by poll and promises a ten-second budget; an
address that is not a chat id or @username is refused before any call;
a send without the bot_token handle is refused by name; a send and a
receive are denied without a grant.

On an identity: install at T2 (the credential floor), grant
`net.outbound:api.telegram.org:443`, `kv` and a credential handle in
`plugins.grants.<id>`, configure that handle as an auth profile with
`"scheme": "path"` pinned to `api.telegram.org:443` whose secret is the
bot token from @BotFather, and set the `bot_token` handle in the plugins
view. The URLs this code writes name `{credential}` where Telegram
wants the token; the broker substitutes it when it dials and this code
never sees it. Add a contact with channel `telegram`, the chat id as the
address (send the bot a message and read `from` in the host's log, or
ask @userinfobot), and wake if that person may wake the identity. The
host records each arrival once by `chat:message_id`, wakes the identity
only for a contact with wake, and delivers the identity's replies
through `send` in the operator's channel order.
