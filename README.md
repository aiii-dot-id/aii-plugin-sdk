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

Descriptors may mark a small set of everyday operations `Primary`. This is an
optional, hash-bound presentation hint. The operator can offer that plugin's
primary operations through the existing tool selection; grants and callable
schemas remain enforced by the host.

With host 0.1.19, `Call.WorkSession()` and resident `Control.WorkSession()` read
the executing task and current tool-call reference from reserved host context
beside the operation's arguments. Argument fields never supply this context;
older handlers continue to receive their unchanged public argument object.
`Memory.Remember` accepts
`MemoryApplicability("task" | "project" | "reusable" | "legacy")` and optional
`MemoryEvidence(...)`. Scoped writes require a host-resolved task; callers do
not supply a project or task ID. Omitting applicability preserves visibly
unscoped legacy behavior. An explicit `Supersedes(id)` corrects a current note;
similar wording alone cannot replace a scoped or evidence-bearing note.

`Memory.Recall` supports `MemorySelection(...)` and `RecentMemories()` as well
as query and `ByID`. Relevant selection combines current-task notes, explicitly
project-wide notes and reusable lessons. Legacy notes require an explicit
legacy selection or an older unfiltered call. Results preserve full text,
context, evidence availability and supersession. Evidence validity identifies
a retained record; it does not grade the authored lesson or task success.
Planning readers can request `InteractionFilter.IncludeDescendants`; incomplete
historical ancestry is disclosed in the source's availability.

Copyright (c) AI Identity Incorporated. Licensed under Apache-2.0; see LICENSE.
