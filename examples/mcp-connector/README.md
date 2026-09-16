# mcp-connector

An MCP connector built with the kit: asks a server for its tools over
JSON-RPC on HTTP, publishes each at run time under this plugin's
envelope, and forwards a published tool's call to the server. The host
admits every published tool like a declared one; the identity finds
them through its tools organ and offers the ones it needs.

    aiisdk build && aiisdk test -grant tools

The cases publish from a given list, then show a published tool's call
and a refresh denied without a network grant, and a missing server as an
honest failure. On an identity: grant `tools` and the server's host in
`plugins.grants.<id>`, set `server_url` and, for a token, a credential
handle in the plugins view; a stdio MCP server is a process outside
containment and is not reachable from a plugin.

The envelope names the server's host — `mcp.example.test` in `main.go`
and in every variant of `plugin.json`: replace it with your server's
host before building. A signed package reaches only the hosts it
declares, and the operator grants from that list.
