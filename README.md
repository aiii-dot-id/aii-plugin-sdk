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

Host 0.1.20 additionally returns `RecallResult.SelectionContext`, including
empty query/recent results and by-ID misses. This optional host readback names
the selection and only the IDs used by its filter: task/relevant carry work
session and project, project carries project, and reusable/legacy/unfiltered
carry neither. Relevant with `Reason == "no_session"` searches reusable notes
only. Empty `Selection` preserves older unfiltered SDK calls; it does not mean
explicit legacy selection. A nil pointer means an older host omitted this
readback, so do not infer task/project scope. It reports the selected plugin
store, not core memory or the legacy plugin working set. No query, note text,
or caller-supplied scope is echoed in this object.

With host 0.1.20, `Memory.Recall(query, AnyWords(), ...)` explicitly requests
indexed matches of any normalized query word, without fuzzy or embedding
expansion. It requires a nonempty query and cannot combine with `Exact()`,
`RecentMemories()` or `ByID()`. Scope applies before ranking and limits; the
default query behavior is unchanged. A hit's `exact_words` label identifies
the indexed word layer, not agreement with every query word. Ordinary calls
still reinforce access. A bounded candidate set can miss useful notes, and
word overlap alone is not evidence that a lesson helps the current task.

With host 0.1.20, one operation may declare `Context: true` to provide bounded
local context before a turn or child leg. Set the package
`aiios_min_version` to at least `0.1.20`; older hosts do not know this marker.
This optional host-only operation
requires `read.internal`, explicit capabilities and input/output schemas,
`MaxResultBytes` from 1 to 4096, and no `Primary` or `OperatorConfirms` flag.
The initial host contract accepts only supervised, nonresident `wasm_component` packages,
excluding resident sessions, channel adapters and embedding providers. Existing
operation selection, grants, SAFE and activation ownership still govern it.

The input is `{"query":"current request or goal","max_bytes":4096}`; query is
at most 2048 UTF-8 bytes. Return exactly string fields `text` and `detail_route`,
with an encoded result at most 4096 bytes and route at most 512 bytes. Empty
text and route supply no context. Nonempty text needs a normal detail-read
route. Use `Call.WorkSession()` for trusted scope; `no_session` requires an
explicit plugin policy, never a guessed project. Context remains fallible
Ring 4 evidence and the host never executes the returned route.

The host invokes at most two selected providers once per decision, with 250 ms
per provider and 500 ms total including gate wait. It does not retry. Broker
writes, network and embedding requests are refused; automatic memory recall
uses no embeddings or access reinforcement and a 32 KiB response budget
(ordinary recall retains 256 KiB). Match counts/truncation remain explicit; an
oversized by-ID detail fails with guidance to use an ordinary detail read. KV get/list, scoped memory recall,
settings, interaction query/read and sandbox file list/read retain normal
capability checks. Ordinary model-requested recall is unchanged and can
reinforce access. A dispatched timeout can invalidate the WASM instance;
normal supervised worker recovery applies. In-process hosts refuse context
packages before activation: use a supervised host or retain the previous
package. No other invocation is interrupted.
Automatic reads retain broker receipts but are not model tool calls. Supply
cheap bounded relevance and state its coverage; an empty bounded search is
not proof that no useful memory exists.

Copyright (c) AI Identity Incorporated. Licensed under Apache-2.0; see LICENSE.
