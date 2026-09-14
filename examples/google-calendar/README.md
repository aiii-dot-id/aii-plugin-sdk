# google-calendar

A connector built with the kit that reads a Google calendar through a
connected account: what is on today, and the next few events.

    aiisdk build && aiisdk test

The cases prove the connector without the network: a call without the
handle is refused by name; a call with the handle and no grant is
denied and dials nothing.

On an identity: install at T2 (the credential floor); on the Plugins
page, create a profile for Google with the calendar service (read is
enough for this connector) and connect it in your browser — or answer
the identity's connect card, which opens that form prefilled from this
plugin's own hint; grant `net.outbound:www.googleapis.com:443` and the
profile's name as a credential handle in `plugins.grants.<id>`; set the
`google` setting to the profile's name. The host refreshes the token
and injects it at the wire; this code never sees it. A Google project
in Testing status expires its refresh tokens after seven days, so
reconnect weekly until the project is verified.
