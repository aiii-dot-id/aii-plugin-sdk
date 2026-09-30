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

Copyright (c) AI Identity Incorporated. Licensed under Apache-2.0; see LICENSE.
