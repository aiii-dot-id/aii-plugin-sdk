# AII plugin SDK

The SDK provides a guest library, package tooling and examples for AII OS plugins.
Required cryptographic dependencies retain their upstream module names.

```sh
go build -o aiisdk ./cmd/aiisdk
./aiisdk init -sdk . -dir /path/to/new-plugin com.example.hello
```

Use `aiisdk --help` and the examples for the authoring interface. WebAssembly
guest builds require TinyGo; native examples use Go. Use a development signing
root for local testing and keep signing private keys outside source repositories.

`aiisdk package` includes optional `LICENSE`, `NOTICE` and
`THIRD_PARTY_LICENSES` files from the plugin's root directory unchanged in
`install-root/`. These files are covered by the canonical package hash and
signatures. They must be regular files; symbolic links and other file types are
refused. Authors remain responsible for supplying the applicable notices.

Copyright (c) AI Identity Incorporated. Licensed under Apache-2.0; see LICENSE.
