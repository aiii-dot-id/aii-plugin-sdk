# Writing a plugin

From an empty directory to a plugin the host **verifies** and **runs** —
using only this repo and the host's binary oracles. Every command below
is real and copy-pasteable; the sequence is exactly what
`acceptance/` proves on each run.

## What you need

- **Go 1.25 or newer.**
- **TinyGo 0.42 or newer** (https://tinygo.org) to compile the guest
  wasm. TinyGo drives the Go on its PATH and accepts 1.25 through 1.27.
- For the *verify* and *run* steps, the host's `aii` in a directory
  named by `AII_OS_BIN` — from an AII OS release
  (https://github.com/aiii-dot-id/aii-os/releases) or built from its
  source:
  - `aii plugin verify` is the offline package verifier;
  - `aii plugin-worker` runs one wasm module behind the sandbox (a host
    checkout also builds it standalone, `aii-plugin-worker`, which the
    kit finds on PATH).

  The kit never imports them, it only executes them. You can author,
  build, and package a plugin with neither present; you need them to
  *prove* the result.

## 1. Install the CLI

```sh
go install github.com/aiii-dot-id/aii-plugin-sdk/cmd/aiisdk@latest
```

Or clone the kit and build it: `go build -o bin/aiisdk ./cmd/aiisdk`.
`aiisdk` is the whole authoring surface: `init`, `build`, `package`,
`test`, `devcert`, `sign`, `revoke`, `publish`, `runtime-pack`,
`version`. `aiisdk help` is the map.

`aiisdk version` says which kit you have, on one line, from what the Go
toolchain recorded in the binary: the module; its version — the one
`go install` fetched (a release's tag, or a pseudo-version naming the
commit), or, built in a checkout, the tag it sits on or a pseudo-version
naming the commit (ending `+dirty` if the checkout had changes), or
`(devel)` from `go run` or `-buildvcs=false`; and, for a build made in a
checkout, the commit and whether the checkout was modified. What `go
install` builds carries no commit stamp, and the line says
`revision=unknown` and why rather than guess one.

## 2. Scaffold a plugin

```sh
aiisdk init helloworld
cd helloworld
```

`init` writes a project that compiles with the SDK as its only import:

| file          | what it is                                                    |
|---------------|---------------------------------------------------------------|
| `plugin.json` | the authoring file — identity, interface, variants (closed: a typo rejects) |
| `main.go`     | your handlers; they register in `init()`, never `main()`      |
| `go.mod`      | wired to the SDK (a local `replace` when `-sdk` names a checkout) |

An `aiisdk` installed from a release writes that release into `go.mod`.
One built from a checkout wires the checkout in with a `replace`
directive: pass `-sdk <path>`, set `AII_SDK_DIR`, or run it from inside
the checkout.

`init` seeds one operation, `core.echo`, that returns its arguments
unchanged — a plugin that already builds, packages, and runs.

## 3. Write a handler

Open `main.go`. Registration lives in `init()` — the wasm host runs only
the module initializer, never `main()`. Replace the echo with something
of your own:

```go
func init() {
	p := sdk.New("helloworld")

	p.Describe("greet", sdk.Descriptor{
		Summary: "Greet a caller by name, or the world",
		Effects: sdk.EffectsReadInternal,
	})
	p.Handle("greet", func(c sdk.Call) (any, error) {
		// Read arguments with the strict-domain walker, which reads what
		// the wire allows and nothing looser. (json.Unmarshal into a
		// struct also runs in a guest on TinyGo 0.42.0, by encoding/json's
		// looser rules; earlier releases trapped.)
		name, ok := c.Args().String("name")
		if !ok || name == "" {
			name = "world"
		}
		return map[string]any{"greeting": "Hello, " + name + "!"}, nil
	})

	p.Run() // last, still inside init()
}

func main() { sdk.MainDescribe() }
```

Two rules the library enforces for you:

- **Return maps, slices, primitives, `json.RawMessage`, or a value with
  `MarshalJSON` — not a plain struct.** Struct marshaling needs
  reflection TinyGo's wasm target cannot run; the error message says so.
- **Report expected failures with `sdk.Fail(reasonCode, msg)` or
  `sdk.Deny(...)`.** They become the audited failure result the host
  reads; a bare `error` becomes an internal-failure object.

And one rule the library CANNOT enforce for you:

- **IGNORE ARGUMENT KEYS THAT BEGIN WITH `_host`. EVERY CALL CARRIES AT
  LEAST ONE.** The `_host*` namespace is host-reserved: the host drops any
  `_host*` key a caller supplied, then writes its own. Every operation call
  arrives with `_host_now_ms`, because a guest has no ambient clock by
  design and trusted time reaches you as data. An operation you declared
  `OperatorConfirms` also arrives with `_host_operator_act` once the
  operator confirms it. A plugin that declares webhooks arrives with
  `_host_webhooks` too: where each of its own is mounted (§13). So a call the
  identity made with NO arguments still
  reaches you as `{"_host_now_ms": 1757…}` — you will never receive an
  empty object.

  A parser that refuses unrecognised keys will therefore refuse every call
  it is ever given, including the ones that look empty. That is not
  hypothetical: a resident speech engine shipped an enrollment family whose
  parser was strict, and a live identity got the identical "unknown
  argument" error seven times running — on a read operation that takes no
  arguments at all — before anyone could see why. Read these keys with
  `c.HostNowMillis()` and `sdk.OperatorAct(...)`, and let anything
  else beginning with `_host` pass without complaint.

`main()` stays a one-liner. Your `Describe` calls are the one source of
the descriptor surface, and it reaches the package through two doors
that must agree: the built module exports it (`aiii-plugin-describe`),
which `aiisdk package` reads through a worker oracle when it finds one
(`AII_OS_BIN`, or `aii-plugin-worker` on PATH), and `sdk.MainDescribe()`
prints the same bytes when running your package under a scrubbed
environment is the fallback. That emission *is* the interface schema
file the package ships, and for a WASM artifact declaring one core
interface the host asks the artifact for its own account at activation
and refuses a package whose file says something else (a native variant,
or a package with two core interfaces, is taken as declared). Declare
each operation once, in Go; nothing duplicates it.

`Descriptor` also takes `Input` and `Output` (paths of the operation's
JSON Schema files, kept to the closed subset in
`vectors/schema_subset.json` — `type`, `properties`, `required`, a
boolean `additionalProperties`, one `items` schema, `enum`, `const`, the
numeric and length bounds, `pattern` in RE2 syntax; no `$ref`, no
combinators), `Capabilities`, and `MaxResultBytes`, the bound the host
holds this operation's result to.

`Input` and `Output` are optional. Omitting one leaves that side without a
schema check; a positive `MaxResultBytes` adds a result cap, while zero leaves
the enclosing frame limit. Schema validation and package/ABI verification are
separate checks. A path you declare is not optional: the host refuses to
activate a package whose declared schema is missing, cannot be read as the
verified bytes, or leaves the closed subset, and a refusal caused by the
package's own declaration waits for a corrected package instead of being
retried. Omitting the path is the only way to leave a side unchecked.

For a single-interface package, `plugin.json` declares the interface id
and version; its method list is derived from your `Describe` calls. Do not
maintain a duplicate operation list. For a multi-interface package, use
`interfaces` instead of `interface` and declare each interface's methods as
a partition of the described operations. The packager requires every
operation to belong to exactly one interface. This lets a native speech
engine keep host-only session controls separate from ordinary tools.

## 4. Build the guest module

```sh
aiisdk build
```

This runs the pinned recipe — `tinygo build -o dist/helloworld.wasm
-target=wasm-unknown -scheduler=none -gc=conservative -no-debug .`. The
output is `dist/helloworld.wasm`.

## 5. Package the canonical bundle (T0)

```sh
aiisdk package
```

`package` reads your descriptor surface from the built module's own
account (through a worker oracle, or by running your package under a
scrubbed environment when none is found), checks every schema file
against the closed subset, stages the canonical tree under
`dist/pkg/helloworld-0.1.0/`, computes the
exact-release quartet (package/manifest hashes, per-file schema and
artifact hashes) from the staged bytes, and writes
`dist/helloworld-0.1.0.aiiospkg`. It is **unsigned — trust tier T0**.

## 6. Prove it: verify and run on the host

The one-command form is `aiisdk test`: it builds, packages, verifies with
the host's own verifier, holds the packaged descriptor to the module's
account, runs the module on the worker under the grants you name
(`-grant kv`, `-grant net.outbound:api.example.com:443`) and drives the
cases in `tests/*.json` — the scaffold ships one for `core.echo`. The
module it runs is the one the package carries for the wasm variant it
reports on (its `artifact` row names the variant, the member and its
hash): a variant that names its own `"artifact"` is tested on those
bytes, not on the module `aiisdk build` left in `dist/`. The
host calls your handlers make are answered by a stand-in host whose
storage dies with the run and whose observations are printed as such:
they are not receipts, and no receipt is written. It needs the
distributed host binaries (`AII_OS_BIN`, or `aii-plugin-worker` and
`aii` on PATH); without them it ends INCOMPLETE (exit 3), never as a
pass. The pieces, by hand:

Verify the package with the host's own offline verifier:

```sh
aii plugin verify dist/helloworld-0.1.0.aiiospkg
# VERIFIED T0 helloworld 0.1.0
#   package_hash  sha256:...
#   manifest_hash sha256:...
```

Run the module and drive your operation over real BBB frames. The worker
speaks 4-byte length-prefixed frames on stdin/stdout; one request in, one
response out:

```sh
aii-plugin-worker dist/helloworld.wasm
# stderr: aii-plugin-worker: event=ready ... bbb_protocol_version=2
```

The request is a JSON-RPC `invoke.call` naming your operation; the reply
carries `operation_result`:

```json
{"jsonrpc":"2.0","id":1,"method":"invoke.call",
 "params":{"operation":"greet","arguments":{"name":"Recipient"}}}
```
→
```json
{"jsonrpc":"2.0","id":1,"result":
 {"status":"succeeded","operation_result":{"greeting":"Hello, Recipient!"}}}
```

Closing stdin ends the session and the worker exits 0. (The kit's own
`acceptance/` test drives exactly this loop end to end.)

That is the **T0 loop, complete**: cloned, built, packaged by the SDK's
own writer, VERIFIED by the independent host verifier, and RUN on the
host.

### The qualification report

`aiisdk test` is the local qualification path, and `-report` writes what
it checked as a machine-readable file an agent or CI can read:

```sh
aiisdk test -report qual.json
```

Every row carries a **status** — `pass`, `fail`, `incomplete`, `not_run`,
or `note` (informational, e.g. no test cases present) — and an **evidence
class** naming the scope the result was observed at:

- `local` — an observation this command made on this machine;
- `locally_verified` — a check read back here: the host verifier's
  `VERIFIED` line, the packaged descriptor equalling the module's own
  account, the `package_hash`/`manifest_hash` the signature will bind;
- `host` — a boundary this command does **not** cross.

That last class is the point: a green local run is not a deployment. The
report always names the host boundary and never crosses it —

```json
{ "name": "signature",             "status": "not_run",    "class": "host",
  "next": "run 'aiisdk sign' to mint the T1-under-dev-root signature — still local evidence, never a host receipt" },
{ "name": "host-activation",       "status": "not_run",    "class": "host",
  "next": "activate on the host, then read the result back through the host's verified channel" },
{ "name": "host-receipt",          "status": "not_run",    "class": "host",
  "next": "obtain the receipt from the host after activation — only host-produced data is deployment evidence" },
{ "name": "manifest_hash-consumer","status": "incomplete", "class": "host",
  "next": "run 'aii-devsign -payload-out' on the staged manifest.json, read manifest_hash back, and compare — fail closed on mismatch" }
```

— and no local observation can ever fill a `host` row. `manifest_hash-consumer`
is `incomplete` on purpose: the SDK reports the `manifest_hash` its own
canonicalizer computes (`aiiospkg.ManifestHash`), but the signing consumer
(`aii-devsign`, built from the host's source) may canonicalize
differently. Until you derive the pair
through the consumer's own payload path and compare, that agreement is
unproven — reported, not assumed.

An `incomplete` report is **not** a release approval. Read each unfinished
row's `next` field: it names the exact command or host-side step still
owed. The shortest truthful path is `build → package → verify → account →
run` locally (all `local`/`locally_verified`), then `sign`, then host
activation and a host-produced receipt (each its own boundary, in order).

## 7. Sign it (T1)

A T1 package is signed by a publisher key that a certifier vouches for.
The real certifier is an AIII authority; for local work you mint your own
**dev** chain, and the package proves T1 to anyone who pins your dev root.

```sh
aiisdk devcert   # once per plugin directory — mints .keys/ (dual-PQ; SLH-DSA keygen is slow); reuse it with sign -keys
aiisdk sign      # stamps the exact-release signature and repacks to T1
```

`devcert` writes a dev certifier root, a dev publisher certified by it,
the pin file `.keys/certifier-root.pub.json`, the publisher
certificate — and the certifier's **empty revocation snapshot**
(`.keys/aiii_plugin_publisher_certifier_status.json`, `trust_epoch` 1).
The runtime fails a signed tier **closed** when its root's snapshot is
missing, so the snapshot is part of the ceremony, not an option. `sign`
recomputes the quartet from the staged tree, signs the closed
`{package_hash, manifest_hash}` payload with the publisher key, embeds
the certificate + signature, and repacks.

Prove T1 by pinning the dev root and handing over the trust dir
(`.keys/` doubles as one — the snapshot sits there under its canonical
installed name):

```sh
aii plugin verify -certifier-key .keys/certifier-root.pub.json \
    -trust-dir .keys dist/helloworld-0.1.0.aiiospkg
# VERIFIED T1 helloworld 0.1.0
#   ...
#   publisher     dev.publisher.<host>
```

Without `-certifier-key`, the same signed package is **refused** — the
publisher evidence never soft-passes to T0. Without `-trust-dir` it is
refused too (`REVOCATION_STATUS_UNAVAILABLE`): a tier whose revocation
state is unknowable is unavailable. Both refusals are the point: tier
comes from verified signatures plus a verifiable revocation posture,
never from a manifest's claim.

### Revoking a release

`aiisdk revoke` pulls a signed payload back out of circulation: it
appends the exact `(artifact_kind, payload_sha256)` pair to the dev
snapshot, bumps `trust_epoch`, and re-signs. The default target is the
staged release's `publisher.sig` (revoking THAT release); revoking the
publisher certificate (`-kind plugin.publisher_certificate -digest ...`)
pulls every release signed under it at once.

```sh
aiisdk revoke
aii plugin verify -certifier-key .keys/certifier-root.pub.json \
    -trust-dir .keys dist/helloworld-0.1.0.aiiospkg
# NOT VERIFIED: TRUST_PAYLOAD_REVOKED [publisher-chain]: ...
```

Revoked evidence **rejects** — it never downgrades to a lower tier. The
epoch bump is anti-rollback: a runtime that has accepted epoch N (a
ledgered fact on the host) refuses any older snapshot as a rollback.

The signing keys are post-quantum (ML-DSA-87 + SLH-DSA-SHA2-256s, profile
`AIII-PQ-SIGNATURE-V1-ROOT`), produced by the kit's one sanctioned
dependency (`cloudflare/circl`) and verified byte-for-byte by the
runtime's own verifiers — `go.mod` says why it is the one dependency. Key material
lives only under `.keys/` (gitignored), in files readable by your account
alone — 0600, and on Windows a DACL of their own, since there the mode bits
do not protect a file; the kit never prints it.

## 8. The other lane: native T3

Everything above builds a **wasm module**. There is a second lane, and a
plugin that needs it looks almost the same.

A **native T3** plugin is a PROCESS, not a module. The host execs it and
speaks the same framed BBB over stdio. That lane exists for code that
must be native — CGO, SIMD, a runtime wasm cannot host. Be precise about
what it does NOT unlock: under the shipped containment the Linux sandbox
(`bwrap --unshare-all`) presents a read-only filesystem, the activation
directory included, no network, and no sound or input devices. Its
device view follows the accelerator profile: a variant whose profile
names the `vulkan` backend sees the GPU's compute nodes inside the wall
— the render nodes and, where present, the NVIDIA device pair, verified
and named in the activation's containment line — while any other
backend, and a native plugin with no profile, computes on CPU over
bundled files there; no NPU is admitted on Linux. (macOS Seatbelt denies network,
file-write and its named SSH/master.passwd paths, so Metal is reachable; the
Windows AppContainer admits the GPU too — its qualification ran a Vulkan
backend inside the wall — a five-platform asymmetry to plan around, not
a promise.) Audio is not on the control lane: a resident speech engine
(§17) receives the session's frames on two descriptors the host hands
it at spawn; every other plugin has no audio at all.

The handlers do not change. Only the last line does:

```go
p.Run()            // wasm: wires the ABI trampoline, called from init()
p.Serve("child-ready")  // native: runs the stdio loop, blocks, called from main()
```

Under `aiisdk package` and `aiisdk test` the packager runs `go run .` with
`AIISDK_DESCRIBE=1`; `Serve` and `ServeReady` answer that by printing the
descriptors and returning, so a native `main` needs no `MainDescribe` call
and no flag of its own — one door, both plugin shapes.

`examples/native-skel` is the whole of it. Build it per platform:

```bash
GOOS=darwin GOARCH=arm64 go build -o native-skel ./examples/native-skel/
```

Three things are different about the lane, and all three are the host's
doing rather than yours:

**One signed package can carry several platforms.** Each variant declares
its platform/architecture and carries its own small executable in the same
`.aiiospkg`. One plugin id, version, settings declaration and signature cover
that archive; the host selects its own variant. Do not install three copies
of the same plugin id. For a large engine, declare one companion runtime per
variant in `runtimes`, and give each variant an accelerator profile naming
its model subset. The host downloads only the selected runtime and models;
the common package includes the small carriers for all declared platforms.
Without a model subset, the host acquires every globally declared model.

For a desktop voice engine, use variants `macos/arm64`, `linux/x86_64` and
`windows/x86_64`, each with `execution_runtime: native_t3_component`, its own
`artifact`, a matching `runtimes[].variant_id`, and `accelerator.models` names
from the global `models` list. Exact shared model bytes can appear in more
than one subset. Different model bytes need different destination paths.
Platform selection does not waive containment or OS/CPU compatibility gates.

**Resource-aware component sets (requires AII OS 0.1.12).** A package may
declare `"variant_preference": ["linux-full", "linux-small", ...]` together
with `"aiios_min_version": "0.1.12"`. List every variant exactly once (up to
64); omission keeps the older default/ambiguity rule. The signed order decides
among eligible sets, not among individual models. A smaller set may use the
same carrier with different runtime and model declarations, or its own carrier.

Every native set in that package needs a matching accelerator profile with
`memory_bytes`, explicit `device_memory_bytes` (zero for CPU or unified memory),
and `required_accelerators`, if any. For example, a CUDA set can declare
`"required_accelerators": ["cuda"]` and a discrete device reservation. A CPU
set omits the requirement list and declares zero device memory. Names are open;
unknown names are unavailable, never guessed. `backend` still describes the
implementation and is not reinterpreted as a hardware requirement.

The host measures available capacity, applies the operator's host-memory reserve
and budget, filters, then follows your order. It acquires only that set's runtime
and model subset. Corrupt declarations refuse; they are not fallback triggers.
Capacity is rechecked before startup after downloads. Aggregate admission,
containment and real inference readiness still decide whether it can run.

The initial desktop collector measures host memory, default NVIDIA device 0's
CUDA memory, and Apple Silicon Metal sharing the host pool. It does not infer
Vulkan, DirectML, ANE or another placement from a model name or installed VRAM.
Such requirements remain unknown until measured. Unified memory belongs entirely
in `memory_bytes`; do not add a separate device reservation for the same pool.
Alternative placements and every component combination need their own retained
qualification; this selection contract does not qualify a Small engine or mobile
package on the author's behalf.

**The child is contained on every desktop.** The mechanism differs by
platform; the property does not: no network of its own, nothing
writable beyond what the host hands it, and it dies with its supervisor.

| | native T3 containment |
|---|---|
| Linux | bubblewrap: read-only root, `--unshare-net`; SSH and shadow files masked, other user-readable credentials remain readable |
| macOS | Seatbelt via `sandbox-exec`: no network or file writes; SSH and master.passwd denied, other user-readable credentials remain readable |
| Windows | an AppContainer with no capabilities — no network — inside a job object: dies with the supervisor, no breakaway, UI-restricted; it reads what the host grants it and writes only its own container folder |
| Android / iOS | unreachable: mobile T3 is in-process bundled, and iOS forbids exec |

**Write your plugin as though the wall is there**: on the three desktops
it is. But do not rely on it to contain a bug: a wall bounds what a child
can reach, not what it does with what it reaches — and on macOS the
child can still read most of what its user can read. Linux also exposes a
broad read-only root. The named OS credential masks are not a complete
credential boundary: readable AWS, GnuPG, gcloud, netrc and kubeconfig files,
for example, remain reachable. A broker credential handle prevents credential
export through the API; it does not revoke native filesystem access. This is
the host's loading tradeoff, and what you load, cache and log remains yours to
handle correctly.

**Devices on the operator's own network** are a class of their own: a
plugin declares `net.local` bare in its envelope —
it cannot know the operator's devices at packaging time — and the
operator names them in `plugins.grants.<id>.local`: an address, a range
like `192.168.1.0/24`, or a name, each with `:port` or `:*` for any.
The grant is the consent; no tier ceiling stands above it. A granted
name must resolve to the local network and is dialled at the addresses
it resolved to, never a second resolution. A credential handle may ride
plain http to a local device at the operator's one-word consent
(`plaintext_credentials: true`), since most local services speak plain
http and take a token. Never admitted whatever the grant says: the
identity's own listeners and the cloud metadata address. Test it the
same way: `aiisdk test -grant net.local:192.168.1.10:8123`, or a range,
or a name; a public address is refused at parse.

Every external effect should go through the broker regardless —
`net.outbound` means an `http.get` hostcall, never a socket you open.
That is the contract whether or not the platform is enforcing it, and it
is the reason a plugin written honestly stays correct when the wall
arrives. Signing proves provenance, not correctness.

**Readiness is a report, not a mark.** The host waits for a readiness
line only when the variant declares an accelerator profile, and then
only for the `event=ready` line that `ServeReady` prints — models
loaded, the accelerator, one bounded inference, and the kit that built
the plugin (section 15) — before it sends work;
a plain `Serve(mark)` writes its mark to stderr, where it is logged, not
waited for, and work can arrive as soon as the process exists. Either
way, call `Serve` or `ServeReady` AFTER the models are loaded: ready
means ready, not started.

Hostcalls work inside a handler exactly as they do in wasm, including
while the host is waiting for that handler. The SDK reads the reply
inline; there is no concurrency for you to manage, and the control pair
is unbuffered and flushed per frame because a buffered writer here
deadlocks that wait.

## 9. Two memories, one kit

The kit ships two working memories for an identity, built as plugins
the way yours will be — scaffold-shaped, with `plugin.json`, schemas,
and `tests/` cases — and they are the memory slice's acceptance:

- `examples/memory-embeddings` embeds every remembered text through the
  identity's **own provider** (`sdk.Embeddings.Create`, the host's
  `embeddings.create`) and recalls by cosine similarity. It requests
  `ring4.kv` and `model.embeddings`; the operator grants `kv` and
  `embeddings` in `plugins.grants`, and names an `embeddings_model` on
  the provider. Under the harness the vectors come from a hashed-words
  stand-in: enough to prove store-and-recall, nothing about meaning.
- `examples/memory-keywords` stores each text with its word set and
  recalls by word overlap. It needs `ring4.kv` only, runs at T0 with a
  kv grant, and is the honest floor to compare the first against.

Both enumerate their own scope with `sdk.KV.List(prefix, limit)` and
report a denied host call as a **denied result** (`sdk.Deny` with the
host's reason code) rather than a payload that says it succeeded — the
host holds a success to your declared, loaded output schema, and a denial is classified
by its reason, never hidden. Run either:

```sh
cd examples/memory-embeddings
aiisdk build && aiisdk test -grant kv -grant embeddings
```

A third way needs no index of your own. The host keeps memory
instruments of its own: `sdk.Memory.Remember(text)` records
a text into your plugin's own record and answers what the record decided
it was — `created`, `reinforced` (a memory this similar was already held)
or `updated` (a near-duplicate, or the memory you named with
`sdk.Supersedes(id)`, is superseded and kept for audit) — and
`sdk.Memory.Recall(query, opts...)` searches it: every word must appear,
fuzzy matches tolerate a typo, meaning matches join when the identity's
provider names an embeddings model (a hit carries its `Similarity`, and
the result's `Meaning` says `found`, `found_nothing` or
`source_unavailable` with the reason — never silently absent), hits are
ranked by match and decayed by age and use, and the result says `found`,
`found_nothing` or `partial` with the counts. Options: `sdk.Exact()`, `sdk.Since(t)`, `sdk.Before(t)`,
`sdk.Limit(n)`, `sdk.ByID(id)`, `sdk.Decay(sdk.DecayNone)` for pure
retrieval or `sdk.Decay(sdk.DecayACTR)` for ACT-R activation instead of
CARRD. The host stamps the provenance (your plugin, the time, the
identity's active project); you never see an index. Request
`ring4.memory`; the operator grants `memory`; the harness answers under
`-grant memory` with a temporary record that matches words and
substrings and applies no decay, and says so in its observations.

For **operator-visible interaction history**, declare `interaction.read` and
have the operator grant `plugins.grants.<id>.interactions`. Memory and KV grants
do not imply access. `sdk.Interactions.Query(sdk.InteractionQuery{...})` returns
bounded, versioned pages from `recorded` or `transient` sources. Preserve source
incarnation and cursor; changing filters starts a new query. Equal text may name
different occurrences. Legacy, transient, missing and pruned data are explicit,
not zero-valued evidence of success. These reads do not reinforce memories.

When a row advertises `ContentRef`, use `sdk.Interactions.Read` with its ID,
source, incarnation and SHA-256. Advance the byte offset by the returned byte
count and decode only after assembling the desired bytes; a range may split a
UTF-8 character. The maximum decoded range is 16 KiB. A changed detail/source or
revoked grant refuses the read, including halfway through assembly. No fallback
to private memory, raw SQLite or filesystem access exists on an older host.

The core supplies data; learning and optimization are plugin responsibilities.
The SDK adds no learner, history writer or feedback-annotation API. The native
`examples/interaction-reader` fixture demonstrates the data path through the
real broker. Its `reader.query` operation returns IDs, relationships and selected
work result/evidence fields; `reader.read` returns exact advertised detail bytes.
Build with `go build -o <fixture-path> ./examples/interaction-reader` and run the
host's `TestInteractionIndependentSDKReader` with `AII_INTERACTION_READER` pointing
to that binary. This is an isolated test fixture, not a default-installed plugin.

Capabilities are requested per operation **in the host's own names**:
`ring4.kv`, `ring4.memory`, `interaction.read`, `model.embeddings`, `net.outbound:host:port`,
`voice.observe`. The host holds every host call an operation makes to
what that operation declared (ring 0), so a name the host does not know
is a capability the operation does not have.

## 10. Settings and hints

A connector needs a place for the operator's choices, and a memory
needs a limit the operator can set without a rebuild. Declare them in
`plugin.json`:

```json
"settings": [
  {"key": "recall_limit", "type": "number", "title": "Recall limit",
   "description": "How many texts a recall returns when the caller names no k",
   "default": 3, "minimum": 1, "maximum": 20},
  {"key": "voice", "type": "enum", "title": "Speaking voice",
   "values": ["alba", "ryan"],
   "labels": {"alba": "Alba (Scottish English)", "ryan": "Ryan (British English)"},
   "default": "alba"},
  {"key": "top_k", "type": "integer", "title": "Top-k",
   "default": 40, "minimum": 1, "maximum": 200},
  {"key": "api_key", "type": "secret", "title": "API key", "required": true}
]
```

Six types: `string`, `number`, `integer`, `boolean`, `enum` (with
`values`) and `secret`. An enum declares up to 256 stable values — a
speech engine's recognition locales and its voices are choices, never
free text — and may name each with a `labels` entry the page shows;
the value is what is stored and what you read, the label is only ever
shown, and the page filters a long list without hiding the chosen
value. An `integer` is a number the host holds whole at the view and
at the read: a fraction is refused by key, never rounded, so
`vals.Int("top_k")` always reads. The number of settings (sixteen) and
the number of choices in one setting (256) are separate bounds.
`aiisdk package` writes the block as the package member
`install-root/settings.json`; the host renders it in the plugins view,
holds every value the operator enters to your declaration before it
is stored, and hands your plugin the effective values — your defaults
under the operator's values — when it asks:

```go
vals, err := sdk.Settings.Load() // no capability, no grant: your own configuration
if n, ok := vals.Int("recall_limit"); ok { k = int(n) }
```

Read them inside the handler that needs them; a change in the plugins
view reaches you on your next `Load`. A resident session (a voice
engine, §17) asks the same question through `Session.HostCall(ctx,
"settings.get", nil)` — the host answers it on the lane's own workers,
never through your ordered control handler — and reads it as a session
opens: the page tells the operator a save reaches the next session and
that a running one keeps the values it opened with, so pin what you
read for the session's life rather than re-reading mid-utterance. A
persisted choice your next release no longer declares is named to the
operator on the page and not handed to you; they choose again. **A
`secret` never carries a secret.** The operator chooses one of the credential handles your
grant lets you cite, and `vals.Handle("api_key")` gives you its name to
pass as the `auth_profile` of an HTTP call; the broker injects the
value into that one request and nothing else. When your package's
envelope declares exactly one `net.outbound` host, the operator can
also paste the key on your card: the host keeps it as its own private
file, makes the profile that rides it to that host only, grants it to
you and fills in the setting — you still read a handle.

**Choices your plugin looks up.** A `string` setting may name one of
your READ operations as `choices_from` — a model list your service
knows and your package cannot. The page calls it with no arguments when
the card opens and on Refresh, and offers what it answers as a
drop-down:

```json
{"key": "model", "type": "string", "title": "Model", "default": "jev-latest", "choices_from": "models"}
```

```go
p.Describe("models", sdk.Descriptor{Summary: "List the models the service offers",
	Effects: sdk.EffectsReadExternal, Capabilities: []string{host}, …})
// the handler answers {"choices": [{"value": "jev-latest", "label": "Jev (latest)"}, …]}
```

The operation must be one you describe (`aiisdk package` refuses
another) and a read — the host never calls a write to draw a page. The
answer is held to an enum's bounds (256 choices, unique values, labels
of 64 bytes) and refused whole. The choices are an offer, not the rule:
the operator's value is any string the setting admits, and a failed
lookup leaves the text field with the reason.

`aiisdk test` answers `settings.get` from your declaration: defaults,
then `-setting key=value` for the run, then a case file's own
`"settings": {...}` for that case — so `examples/memory-embeddings/
tests/07-recall-limit.json` proves the operator's limit is honored
without touching the other cases. A key you never declared fails the
run by name, as the host would refuse it at the view.

**An act the operator must confirm.** An operation that changes
something on the operator's behalf — enrolling a speaker, removing one,
resetting a store — declares `OperatorConfirms: true` in its descriptor.
The host then never dispatches it from a tool call on the identity's
word alone: the identity's call PROPOSES it, the plugins page shows the
operator the exact arguments
(and, for arguments that name a session's transcript finals, the text
that was heard), and the operation runs once, with exactly those
arguments, when the operator confirms — or at once, under a standing
"Always" the operator gave ahead of time, stamped as that act. Your
handler reads the host's
stamp — `sdk.OperatorAct(args)` gives the act's id and time — and
refuses a call without one, because the host never makes one. A
denial, a second confirm of the same act and changed arguments never
reach you. In `aiisdk test`, a case with `"operator_act": true` arrives
stamped; one without it must see your refusal.

`aiisdk test` also fails an operation that writes to a third party
(`write.external`) and neither declares `OperatorConfirms` nor is excused:
the host would dispatch it on the identity's call, with no card, and an
omission should not be able to ship that. Where no operator's yes is the
design — a draft that reaches no one, a request that changes nothing at the
service — mark one of the operation's cases `"unconfirmed_by_design":
"<why>"`: a choice written down with its reason. A mark on an operation the
descriptor contradicts (confirmed, not an external write, not there) is a
finding too. A channel adapter's operations are exempt: the host refuses
`OperatorConfirms` on them, a contact being the consent.

**Hints.** A descriptor may add `Family`, `Keywords` and `Examples`.
Installed plugins are not prompt context: the identity reaches your
operations through its tools organ — a brief by family, a search by
need, one operation shown whole, then an offer that makes it callable.
The family defaults to your operation id's first segment
(`memory.recall` → `memory`) and the search text to the id, the
summary and your plugin id; hints sharpen what a search finds and what
a show renders. Keep the summary the sentence you would want the
identity to read first.

## 11. Reaching out: verbs, effects, streams

`sdk.HTTP` speaks GET, POST, PUT, PATCH and DELETE, all under the same
grant (`net.outbound:<host>:<port>` in your envelope, your descriptor's
capabilities, and the operator's `plugins.grants.<id>.hosts`). A write
needs `write.external` on its descriptor; the host refuses a post from
an operation that declared a read.

```go
opts := &sdk.HTTPOptions{Body: payload, ContentType: "application/json",
    Headers: map[string]string{"Accept": "application/vnd.github+json"}, AuthProfile: token}
res, err := sdk.HTTP.Post(url, opts)
```

**The effect is on the receipt, and you must read it.** `res.Effect`
says what the host observed: `performed` (a response arrived, whatever
its status), `not-performed` (nothing was sent), `unknown` (the request
was written and the response was lost). `sdk.EffectUnknown(err)` is the
one failure a write must not retry blindly — the remote effect may
have happened. Declare `Idempotent: true` only when it is true, and the
host retries once for you. A mutation's redirect is its answer, not a
place to go, unless you say `FollowRedirects`.

**A write the operator confirms says what the service did.** An
operation declared `OperatorConfirms` (§10) runs once when the operator
confirms it; the host records its answer for the identity and never
repeats it. Answer in the codes §18 gives a channel's send: a result when
the service took the write; `NET_EFFECT_UNKNOWN` when it may have (a lost
answer, a 5xx, a success you cannot read); `NET_REMOTE_FAILED` when it
accepted nothing; `OPERATION_ARGUMENT_INVALID` when these arguments will
not be carried out; the host's refusal relayed with its code. Asking again
is a new act, and it should find what the first did rather than do it
twice: `examples/google-calendar` derives an event's id from the intended
event and reads a collision back before claiming it, binds a change to the
version it read with `If-Match`, and reads a delete's event before and
after. A service that takes no client id and refuses no second copy leaves
only a search, and a search that finds nothing is not proof:
`examples/gmail` derives a send's Message-ID from the intended mail and
searches for it before sending, so the operator's confirmation licenses a
send and the search can only stop one. Hold every answer to the output
schema you declared: a result the host refuses is a refusal of the answer
only — the operation ran — so for an operation that may write it says the
effect on the service is not known (`PLUGIN_RESULT_REFUSED`, receipt effect
`unknown`), and the receipt of a write that happened is lost.

**When to ask again is the service's word.** An answer whose Retry-After
the host can read carries it as `res.RetryAfterS`, whole seconds:
delta-seconds as the service sent them, `0` included, or an HTTP-date
as the seconds until it by the host's clock, rounded up. It rides only
an answer that arrived (`performed`), whatever its status, and changes
nothing else: a 429 is still a failed remote outcome with its status,
and the request was sent. `nil` is no hint — no header, a past date, a
malformed or overflowing value, or a host that writes none (an older
host, or `aiisdk test`'s stand-in) — never "ask again now"; on a 429 or
503 the host's receipt says the hint was unavailable. The host repeats
nothing on it, and it never licenses repeating a request whose effect
is unknown: a hint of 0 on one answer says nothing about a send whose
answer was lost.

**A credential is a handle.** Never put a token in a header: the
broker refuses `Authorization` from a call. The operator configures an
auth profile, grants you its handle, and names it as a secret-typed
setting; you cite the name as `AuthProfile`, and the host injects the
secret into that one request at a review-proven tier.

**Streams.** `sdk.HTTP.Stream("GET", url, opts)` returns the response
head; `Next()` pulls numbered chunks of at most 64 KiB, nothing read
ahead of you; `Close()` ends early; the exchange has one receipt,
written at its terminal outcome — and your operation's end closes what
it opened. `examples/github-issues` is the connector these lessons
come from: its hermetic cases prove the denials and its argument
checks without the network, and its live cases list a public
repository's issues with one grant.

## 12. Files: your own directory, and the identity's sandbox

`sdk.Files` gives you two kinds of place. Your **private directory**
needs no grant: declare `fs.private`, and `sdk.Files.Write(sdk.PrivateRoot,
"index.tsv", data)` lands under the identity's data directory in a
folder of your own, which lives as long as your release does at a
publisher-proven tier and dies with the activation below it, like RING4.
The **identity's sandbox** is the world the identity itself works in:
its home, and the folders its operator added in Settings → Sandbox.
Declare `fs.sandbox`; once the operator ticks **files** on your card
(publisher-proven tier), you reach it by the paths the identity's own
tools take — `sdk.Files.ReadAll(sdk.SandboxRoot, "projects/notes.md",
limit)`, or an absolute path inside an added folder — so a path the
identity hands you in a call is one you can open as given. There is no
list of folders of your own: the operator widens the world once, for the
identity and every plugin it grants.

The host holds every path to one rule, the sandbox's own: outside the
identity's world is refused (`FS_OUTSIDE_SANDBOX`), and so is its
substrate — the data directory, key, store, config and installed
plugins (`FS_PROTECTED`) — wherever the path is spelled and wherever a
symlink leads. In your private directory a path is relative, with no
traversal and no symlink followed. Nothing you write is ever executable;
reads come in pieces under the response ceiling, writes in pieces under
the request ceiling, and your private directory and what one activation
writes into the sandbox are held to the resource envelope's files
ceiling. Whether you may write in the sandbox at all is the operator's
**read only** box.

**A file that must never be half-written** — a store you keep whole, an
enrollment, a snapshot — is published, not written: `sdk.Files.Publish(
sdk.PrivateRoot, "snapshot.json", data, digest)` replaces the file
atomically — the new bytes are complete, synced and measured before they
take the name, in one rename; the prior file is untouched by any
failure and nothing partial ever has the name. `digest` is the
generation you mean to replace: the `sha256` a whole `Read` or
`Digest` carried; a file that is not that is refused
(`FS_GENERATION_MISMATCH`) with the digest in place, so a lost
concurrent update is impossible — re-read, reconcile, publish again.
`PublishNew` publishes only a first file. Content over the request
ceiling is staged first: `Append` it in pieces under a staging name,
then `PublishStaged(root, path, staged, sha256OfWhole, digest)` — the
host refuses a staged file that does not measure to the digest you
declared and leaves it for you. One publisher at a time per path, across
the old and new activations of an update. The result says whether the
publication is durable and how (`synced`; `file-synced` on Windows;
`unknown` when the directory sync failed — published, not proven). From
a resident session the same call is `Session.HostCallTo(ctx,
"fs.publish", {root, path}, args)`.

`aiisdk test` answers the same calls: the private root is a temporary
directory of the run, and `-grant files=sample` stands `sample` in for
the identity's sandbox (the harness has no substrate to keep; the host
does). `examples/document-ingest` is the plugin these lessons come from:
it ingests a file from the identity's sandbox into RING4, keeps a
private index, and its cases prove the refusals.

## 13. Push from the outside: webhooks

A protocol that pushes — Twilio, GitHub, Telegram — reaches your plugin
through a webhook you declare in `plugin.json`:

```json
"webhooks": [{"path": "sms", "operation": "sms.inbound",
  "signature": {"scheme": "twilio", "header": "X-Twilio-Signature", "secret_setting": "auth_token"}}]
```

The host mounts it at `https://<public name>/hooks/<plugin id>/sms` and
**verifies the signature before your operation runs**, with the secret
behind the credential handle your `auth_token` setting names — a secret
you never see. Four schemes cover the senders that matter: an HMAC over
the body in hex or base64 (GitHub's `sha256=` prefix is a `prefix`),
Twilio's HMAC over the URL and the sorted form, and a plain token
(Telegram). A signature is required: the public origin is public.

Your operation reads the request with `sdk.ParseWebhook(c)` — method,
path, query, a few headers, the body, and `Form()` for a form-encoded
one — and returns `sdk.WebhookResult{Arrival, Response}.Value()`. The
host records the arrival once by its ID and carries it to the identity
through the channel seam, exactly as a received message: wrapped as
foreign text, waking only a contact the operator granted wake. The
response is what the caller receives (Twilio wants TwiML).

A service you must point at your webhook — Pub/Sub's push subscription, a
provider's callback setting — needs its address, and the host is the only one
who knows it. Every call to a plugin that declares webhooks carries
`_host_webhooks`, an object of path to URL for that plugin's own; read one with
`c.WebhookURL("sms")`. It answers `false` while the identity has no public
origin, and the operation then says so instead of registering an address that
cannot work (`examples/gmail`'s `gmail.setup` does).

A channel adapter that hears by webhook says so — `describe` returns
`"receive": "webhook"` — and the host never polls its `receive`.
`examples/twilio-sms` is the adapter these lessons come from; its cases
prove the adapter without the network, and the acceptance packages its
declaration. `aiisdk test` invokes a webhook operation with
request-shaped arguments; the signature is the host's to verify, so
the harness does not.

## 14. Events, and tools you publish at run time

**Subscriptions.** Declare in `plugin.json` which of the identity's
events you want delivered — `tool.called`, `turn.started`, `turn.ended`,
`ledger.appended`, `alarm.fired`, and the work boundaries `work.started`,
`work.delivered`, `work.harvested`, `work.graded` and `subagent.spawned` — and the
operation to invoke, with an optional exact-match filter. Every payload
is identifiers and classes: `tool.called` carries the tool's name, its
outcome and duration, the `actor` (`main`, `safe` or `subagent`) and the
work `session` it acted within; `work.delivered` carries the session,
its project, the actor, the `outcome` class (`served`, `partial`,
`unserved`, `failed`) and the `evidence` class; `subagent.spawned` the
session, role, model, depth and leg. Never a description, an argument,
a result or evidence text — a subscriber learns that the identity acted
and within which piece of work, which is enough to group calls into
runs and cost them, and nothing of what it read or wrote:

```json
"subscriptions": [{"topic": "tool.called", "operation": "log.event", "filter": {"tool": "read"}}]
```

For Go hosts supporting calibration delivery, the authoring declaration may use
`{"topic":"tool.called","operation":"observe","delivery":"calibration_v1"}`.
The packager preserves the profile in `subscriptions.json`; older hosts refuse
it rather than silently falling back to advisory delivery. This profile has one
unfiltered `tool.called` target, with closed schemas, `write.local`, only
`ring4.kv`, and no confirmation/webhook/advisory reuse. It remains a hidden
operation invoked through the existing synchronous `Serve`/`invoke.call` path;
KV hostcalls work normally under its scope. The host SDK contract in
`aii-os/docs/PLUGIN_SDK.md` owns sequence, acknowledgement, trusted context,
loss, restart and completion semantics. The profile requires that host support;
packaging alone is not a calibration qualification.


The host delivers each event as an invocation of that operation, from a
bounded queue that never holds the identity; read it with
`sdk.ParseEvent(c)`. A subscribed operation is the host's to invoke —
the identity never sees it. A tool event carries the tool's name and
outcome, never its arguments; a ledger event the type, ring and
sequence, never the payload. A plugin never owns time: the alarm topic
is the identity's rhythm firing. `examples/logging` is the plugin these
lessons come from.

**Run-time tools.** `sdk.Tools.Publish(spec)` puts a tool in front of
the identity that your package did not declare — an MCP connector
publishing a server's tools — and the host admits it exactly like a
declared operation: a name in the operation grammar, a summary, an
input schema in the closed subset enforced before every call, a known
effect class, and capabilities that must lie within your signed
envelope. It is registered under your prefix, born inspect-only, and
withdrawn with your activation or by `sdk.Tools.Withdraw`. Its calls
reach `p.HandleDynamic(func(name string, c sdk.Call))` under the
published name. Declare `tools.publish` in your envelope and on the
publishing operation; the operator grants it by name; `aiisdk test
-grant tools` answers it on your machine. `examples/mcp-connector`
shows the whole loop.

## 15. The native T3 kit: the profile, the models, real readiness

A native engine declares three things a wasm plugin never does.

**The accelerator profile**, per native variant, in `plugin.json`:

```json
"accelerator": {"os": "macos", "arch": "arm64", "backend": "mlx",
  "operators": ["attention", "conv1d"], "runtime_libraries": ["mlx-0.20", "onnxruntime-1.29.0"],
  "precision": "int8", "models": ["stt-int8.bin", "tts-int8.bin"],
  "memory_bytes": 6442450944, "session_limit": 1, "fallback": "reported"}
```

Its values are your measurements — peak memory, the session count you
qualified, the operators and libraries the artifact needs — never an
operator's constants, and a fallback is reported, never taken silently.
`aiisdk package` writes the profiles as `install-root/accelerator.json`
keyed by variant; the host shows the selected one in the plugins view.

**The models**, declared once and acquired by the host:

```json
"models": [{"name": "stt-int8.bin", "url": "https://…/stt.bin", "sha256": "…", "size": 612345678}]
```

The host fetches each at first activation, verifies every byte against
your hash before the file takes its name, resumes a broken download,
keeps a file the operator placed there when it hashes right, and when
offline refuses the activation naming what is missing. Your process
finds them through `AII_MODELS_DIR`; it never fetches its own, and
nothing fetched is executed.

**A model only some operators need** carries a condition, and the host
acquires it only while that condition holds:

```json
"models": [{"name": "tts-fr.bin", "path": "tts/languages/fr/model.safetensors", "url": "https://…/fr.bin",
  "sha256": "…", "size": 219000000, "when": {"setting": "tts_language", "values": ["fr"]}}]
```

`setting` is the key of an `enum` setting your package declares, and
`values` are one or more of its values. The model is acquired while the
setting holds one of them, by the operator's stored choice or else your
declared default, and not otherwise; nothing is removed when the choice
moves away. No accelerator profile names a conditional model: a profile
lists what its platform always needs, and a conditional model is the
same on every platform. A package that uses `when` declares
`"aiios_min_version": "0.1.15"` or later, since earlier hosts refuse the
member. Your process must expect the files to be absent: open what the
chosen value needs when a session asks for it, and refuse that session
with a plain reason if it is not there yet.

**The runtime**, when your engine is more than one executable: pack its
interpreter, libraries and code — never model data — with
`aiisdk runtime-pack -dir <tree> -platform <platform> -o engine-runtime.tar.gz`,
publish the archive, and declare it per native variant:

```json
"runtimes": [{"variant_id": "windows-x86_64-native", "url": "https://…/engine-runtime.tar.gz",
  "sha256": "<the archive's, as runtime-pack prints it>", "size": 300000000,
  "installed_bytes": 900000000, "files": 15000,
  "inventory_sha256": "<the inventory member's, as runtime-pack prints it>",
  "largest_file_bytes": 250000000, "depth": 6}]
```

Every number is what `runtime-pack` printed for THIS archive: the tar's
digest and size, never a zip's, and the inventory member's digest, never
your engine's own profile file's. `largest_file_bytes` and `depth` are
the archive's extent: its largest file and its deepest member in path
segments below the root. With them the host refuses, before downloading
anything, a runtime whose largest file or depth the operator's ceilings
do not admit; without them it can only find out while extracting. A
package whose `aiios_min_version` is 0.1.14 or later declares both for
every runtime (`aiisdk package` refuses it otherwise); an earlier floor
must omit both, because hosts before 0.1.14 refuse the member's unknown
fields. The host fetches the archive at
activation — never after your process starts — verifies every file
against the inventory the archive leads with, places your carrier at
the root of the tree (beside `python/`, `engine/` and whatever else you
packed: your executable's parent IS the runtime root, and the host adds
no other file to it), and runs it from there with `AII_RUNTIME_ROOT`
beside `AII_MODELS_DIR`; the tree is read-only under the containment
and pinned for the activation's life. Resolve your runtime adjacent to
your executable; the environment name is informational. Nothing under
`AII_MODELS_DIR` is ever executed. The host sets no byte ceiling on a
runtime by default: it holds the tree to the bytes you declare and
installs it only where the free disk holds it, and an operator may set
installed, per-file and compressed ceilings on the Plugins page, which
then refuse a runtime past them before the download. Every byte is
downloaded and kept on the operator's disk, so pack the closure your
engine loads, not everything it was built with. The host's files and
depth ceilings (32,768 files and 24 segments by default; the
operator's to adjust) do apply by default. The files ceiling bounds the
directories too: the host admits the files and a quarter as many
directories again, and `runtime-pack` refuses a tree with more, naming
the least `-max-files` that admits it. It also refuses two names in one
directory that differ only in letter case, as the host does: a
filesystem that folds case would merge them. A runtime that genuinely needs
more files or depth is packed under an explicit budget (`-max-files
40000 -max-depth 30`): the budget is your declared requirement, never
permission, and the report then names as `requires_operator_ceilings`
the settings the operator must raise on the host's Plugins page before
the activation is admitted — state them in your README; `aiisdk
package` repeats them. `-max-installed-bytes`, `-max-file-bytes` and
`-max-compressed-bytes` are your own bounds, held when you pass them.
A model path may lead with a dot (`stt/.gitattributes`): what the host
trusts is the declared hash, not the name. The `<tree>` you pack holds
everything but your executable: the host writes the signed bundle's
carrier at the root of the installed tree, and an inventory that names
that path is refused at install. The packer refuses, by name, what the
host would refuse: a path segment outside `[A-Za-z0-9._+-]` plus
interior spaces and parentheses (so a leading or trailing space, a tab,
a brace, a non-ASCII byte), a trailing dot, a Windows device name, a
symlink, a path over 511 bytes or deeper than 24 segments — so a tree
that cannot ship says so on your machine, not at the host.
`-platform` names the platform the runtime is for, as its variant names
it. A runtime for Windows lists every file as `file`: Windows keeps no
executable mark on a file, so the host refuses an inventory row `exec`
there, after the whole runtime has downloaded. A tree made on a Mac or
on Linux for Windows carries that machine's bits; with `-platform
windows` the packer refuses a file whose executable bit is set, by name.
Clear the bits (`chmod a-x`) and pack again.
`__init__.py`, `__pycache__`, `libstdc++.so.6`, `.dylibs`,
`Lorem ipsum.txt` and `script (dev).tmpl` are all admitted.

**Real readiness.** A variant that declares a profile must report
readiness, not merely spawn: load the models, bring the accelerator up,
run one bounded inference, then

```go
p.ServeReady("child-ready", sdk.ReadyReport{ModelsLoaded: 4, Accelerator: "mlx", ProbeMS: 38})
```

which writes, to stderr,

```
child-ready event=ready models_loaded=4 accelerator=mlx probe_ms=38 sdk=v0.1.14
```

The host refuses a child that spawned without the three fields. The
fourth, `sdk=`, is the kit that built your plugin, read from your
plugin's own build record (what `go version -m` prints), never written
in by the kit: the version your `go.mod` requires — a tag, or a
pseudo-version naming the commit — or `(devel)` while a `replace`
points the kit at a checkout, or `path@version` when it is replaced by
another module; `unknown` if the record names no kit. A host that reads
it names it where it logs the child's readiness; a host that predates
it ignores it, as it ignores any field it does not know. On
Windows the child runs inside the app-container wall, qualified against
a real speech backend before any native T3 was admitted there; a
signature never stands in for the wall.
`examples/native-skel` carries a `plugin.json` with one native
variant per desktop platform and a `build.sh` that cross-compiles all
three; the acceptance packages the matrix and proves the host refuses
it below T3 with `WASM_BASELINE_MISSING` — the platform signature is the
only door for native code, and the kit mints none.

## 16. Publishing to the catalog

`aiisdk publish -url <where-you-host-the-package>` prints one catalog
entry for your plugin: its id, version, an intended tier hint (`-tier`),
a summary (the packaged title or description, or `-summary`), and its
package. A WASM plugin publishes ONE portable package (`"platform": "*"`)
that runs on every host; a native plugin publishes one catalog row per
platform/architecture its variants cover. Every row can point at the SAME
archive URL, SHA-256 and size. Identity, version and coverage are read from
the packaged manifest; a disagreeing `plugin.json` is refused, including
when `-pkg` explicitly selects an older archive. This metadata check is not
signature verification: `-tier T3` is an intended tier hint, not a signature
or proof of admission. The kit
does not host your package and does not sign the catalog: you host the
`.aiiospkg` at the URL you gave, paste the entry into `aiios-plugins.md`
in the platform's `plugin-catalog` repository, and it is signed there
with the platform's release key.

## 17. The resident speech session (voice)

A voice engine is not a series of tool calls: it is a resident session
the host must be able to interrupt while it speaks. `p.ServeSession(admit)`
runs that lane — full-duplex JSON-RPC over the same stdio — in place of
`p.Serve`, for a package whose `plugin.json` declares `"plugin_family":
"voice_interface"`: that declaration is what binds the session lane and
the audio pair at activation; any other family gets neither. Your
`admit` is handed one `*Control` at a time — the eight
controls (`speech.session.open`, `.synthesize`, `.cancel_synthesis`,
`.stop_playback`, `.finish_input`, `.close`, `.status`, `.playback_report`)
in the order the host sent them — and answers it with `c.Answer(result, err)`.
Take the operation and RETURN; answer inline if you know the verdict, or keep
the Control and answer from another goroutine when it arrives. The work runs
on your own goroutines and reports through `c.Session`: `s.Emit(event)` sends
an observation, and `s.HostCall` reaches the broker mid-session. One
observation the host treats specially: `{"type": "speaker_observation",
"session_id", "sequence", "refers_to": <a transcript_final's sequence>,
"speaker", "score", "decision": "known" | "unknown" | "uncertain",
"late"}` amends the transcript turn that final made — the identity reads
the attribution in that turn's channel marker, the page attaches it to
the bubble, a later one replaces the earlier; it is never a new turn and
never authority (the session's role stands whatever you say). The audio
itself travels the host-owned endpoint plane, not this lane.

The contract, in five rules:

- Admission is not completion: a reply says the control was accepted;
  the effect arrives later, as an event.
- Close is admitted into draining (or aborting); the session is closed
  when you emit `session_end` — or a session-scoped `cancellation` — and
  a drain close is refused until `finish_input` fixed the input cutoff
  (an exclusive `end_sample`).
- An interruption is both `stop_playback` and `cancel_synthesis`, each
  admitted on its own; a control that names no `synthesis_id` means the
  current generation (and nothing to cancel is an admission too); late
  output past the cancel fence never revives a reply, and a synthesis id
  is never reused. Output stream ids are the engine's: each reply's
  audio is its own stream under an id the process never reuses (the
  host stamps only the input stream and reads output ids for equality
  alone), so the host can fence exactly the stream it stopped. The open
  names the host's endpoint formats — `audio.input` and `audio.output`,
  each `{rate, channels}`, s16le — and the engine ANSWERS with the
  formats it speaks (`audio.input`/`audio.output` in the admission —
  both, or the open is refused: the host never pumps at a guessed
  rate): the conversion between the endpoints and
  the engine is the host's, at its endpoints, along the sample clock —
  a boundary named in the host's clock reaches the engine as its next
  whole sample. An engine that cannot speak a format refuses the open
  rather than resampling in silence.
- Events carry the session id, a contiguous sequence and a unique id;
  `status` is a bounded snapshot with a `state_sequence`, never a wait.
- Two properties, kept apart: handlers are CALLED in host order, one at a
  time, so an engine can build in one control the state the next one names;
  and no control waits for an earlier control's ANSWER, so a barge-in's stop
  and cancel are never queued behind a synthesis acknowledgement still in
  flight. That is why answering is a method on the Control rather than a
  return value: an engine whose verdict comes from somewhere else — a
  private worker, another process — hands the operation on, returns, and
  calls `c.Answer` when the verdict lands. Its real value or refusal is what
  the host sees. A handler that never returns ends the lane with that
  reason; a control never answered is reported to the host as an unknown
  admission by its own control call, and does not hold the guest open.
- The lane ends honestly: a response the writer cannot deliver, or an
  admission queue the host has saturated, ends the lane and
  `ServeSession` returns the reason. `s.HostCall` says "not sent" only
  when the frame provably never left the process, and "outcome unknown"
  for anything past that — never replay on unknown. Tell them apart with
  `errors.Is(err, sdk.ErrNotSent)` and `errors.Is(err, sdk.ErrOutcomeUnknown)`,
  never by the message; a call the host refused is a `*sdk.Denied`
  (`sdk.AsDenied`), its `ReasonCode` the host's reason.
- Events and outgoing hostcalls backpressure at their share of the single
  outbound queue. Space for all 64 admitted control replies is reserved;
  an answer keeps its admission slot until the writer takes the frame or it
  is explicitly abandoned. The writer frees that reservation before the
  peer can see the reply and send another control.
  An engine's event burst therefore cannot displace an admitted stop/cancel.

`s.Emit` returns once the writer holds the event, not once the host has
it: behind a slow host, many accepted frames can still be queued in your
process. Before you exit on purpose after your final events — a worker
failed and you are handing on its terminal failure, attributions and
`resources_released` — call `s.Flush(ctx)` with a bound, while the lane is
still open. It returns nil only when every frame accepted before the call
(events, answers, outgoing host calls) has been written to the pipe, except
a host call its caller withdrew before the writer took it: that caller was
told it was never sent, and Flush does not wait for it. Frames accepted
after it began are not covered. An error wrapping
`aiiosdk.ErrLaneEnded` means the lane ended or faulted first; one wrapping
your ctx's error means the outcome is unknown and the frames may still be
written. Neither is delivery: report it as unconfirmed, never as sent.

Two more rules close a conversation honestly:

- Input completion is the engine's word, not a transcript. After
  `finish_input`'s exact tail is processed and the recognizer has retired —
  following every final transcript, and for silent input too — emit ONE
  `input_finished` (`stream_id` = the input handle, `end_sample` and
  `processed_end_sample` = the cutoff) and carry the same in `status` as
  `input_completion` (null until then; stable across identical-cutoff
  retries, which re-arm nothing; a changed cutoff is refused). The host
  settles its reply work and admits the final synthesis only after that
  event; never invent a transcript to make a Finish resolve.
- Every output stream is NAMED before it carries audio, and an id is
  used once. Allocate output stream ids from one counter for the life of
  the process — never reset by a new session, never reused, refused
  rather than wrapped when they run out — and introduce each stream on
  the control lane with `synthesis_start` (`synthesis_id`, a strict
  integer `output_stream`) before its first frame. The host binds the
  stream to the session that said that word and routes every audio frame
  by the binding alone, never by which session is listening when the frame
  arrives: audio on a stream nobody named is held a bounded while for the
  word, then discarded, and a word that names a stream already ended — or
  one another session used — is refused and faults the session. Nothing of
  a stream follows its END.
- A session may have NO INPUT, and it says so: an output-only session
  (typed replies spoken with no microphone) is the same `open` with
  `audio.input` PRESENT and `null` and no `input_handle` at all. It is not
  an empty input and not a finished one. Open no hearing path; confirm the
  topology in the admission in the same words (`audio.input: null` —
  a host refuses an admission that leaves the key out, or answers with a
  direction it did not bind); report `input.state: "absent"`,
  `recognition.state: "inactive"` and `input_completion: null`; refuse
  `finish_input`; never emit `input_finished` or a transcript; and let a
  drain close complete on the receipts alone, with no input boundary.
  Every other shape is refused at the open: a handle without its format, a
  format without its handle, an `input_handle` that is null or empty, a
  missing `audio.input`, a missing output. `aiiosdk.ParseSessionAudio`
  reads the open by exactly these rules and `SessionAudio.Admission`
  builds the confirming answer; `vectors/session_topology.json` is the
  runtime's file of examples, and both are held to it.
- An output stream you announce is one you keep a record for. The host
  forwards the page's playback report for every stream it delivered, so a
  stream introduced by `synthesis_start` with no generation behind it
  refuses the very report the host is obliged to send. Register it before
  you announce it, account what it carried and its END, and hold its
  terminal receipt to the rules every other generation's is held to.
- `playback_report` is the host forwarding the page's rendered-sample
  evidence for one output stream: `synthesis_id`, a strict integer
  `output_stream`, a monotonic `rendered_samples` in YOUR output clock, a
  strict boolean `terminal`. Validate it against what you delivered,
  refuse foreign or impossible progress and a terminal on live output,
  keep terminal evidence immutable (an exact retry is idempotent), answer
  changed progress with `playback_observation` labelled client evidence
  (`playback_verified: false`), and complete a drain close only when every
  generation is terminally reported — a missing report is never a
  successful drain.

The audio is not on this lane. A session opened with audio names the
host's endpoints in its `open` (`input_handle`, `output_handle`, and
`audio` — s16le at a rate and channel count), and the host carries the
frames on two descriptors inherited at spawn: `s.Audio()` opens them
once, `Read` takes the next input frame (its stream, sequence and sample
span; a declared discontinuity; an explicit exclusive end that is the
host's `finish_input` cutoff, exact), and `Write` sends an output frame.
`examples/voice-skel` mirrors every input frame to the output so a host
can prove its plane end to end.

`p.DeclareSession()` registers the eight controls as your declared
operations, so `aiisdk package` names them (they are never tools). A
variant with an accelerator profile enters the lane with
`p.ServeSessionReady(mark, report, admit)`, after its models are loaded.
`examples/voice-skel` is the whole shape: an engine with no models and no
audio that keeps every rule, with test knobs on `open` so a host can
prove its own side against it.

**A speech engine that does more than speech declares two interfaces.**
The host decides what an operation IS from the interface that declares
it: `speech.session`'s methods are the host's own lifecycle controls and
never become tools, and everything else you declare is an ordinary
operation the identity can reach. So an engine that also manages speaker
enrollment must NOT put those methods under `speech.session` — they
would disappear. Declare both, and give each its own methods:

```json
{
  "interfaces": [
    { "id": "speech.session", "version": 1,
      "methods": ["speech.session.open", "speech.session.synthesize",
                  "speech.session.cancel_synthesis", "speech.session.stop_playback",
                  "speech.session.finish_input", "speech.session.close",
                  "speech.session.status", "speech.session.playback_report"] },
    { "id": "speaker.uid", "version": 1,
      "methods": ["speaker.enroll", "speaker.list", "speaker.remove", "speaker.reset"] }
  ]
}
```

Use `"interface"` for a package with one, `"interfaces"` for several,
never both. The `methods` lists are a partition of what your plugin
actually describes: `aiisdk package` checks them against the emitted
descriptors and refuses an operation that lands in no interface or in
two, so the lists cannot drift from your `Describe` calls. Each
interface gets its own schema file under `install-root/interfaces/` and
its own `schema_hash`, and every variant implements both. The manifest
grammar's sixteen-method limit is per interface, not per package.

Everything the resident lane carries is still an ordinary operation call,
so **the `_host*` rule from §3 applies to these four exactly as it does
to any other**: `speaker.list` with no arguments reaches you as
`{"_host_now_ms": …}`, and a confirmed `speaker.enroll` reaches you with
`_host_operator_act` beside your own keys. A strict parser here blocks the
whole family.

Mark an operation that changes something on the operator's behalf with
`OperatorConfirms: true` in its descriptor — the host then never
dispatches it from a tool call on the identity's word alone; it records
the exact arguments, asks the operator, and runs it once with those
arguments when they confirm — or at once, under a standing "Always" the
operator gave ahead of time. A read takes no confirmation.

### A publisher's check opens a session too

Your session checks (section 20) open real sessions on your engine before
a host lets anything else reach it, at install and at every start. Such
an open is the host's, never your package's, and it says what it is:

```json
{"session_id": "check-1", "check": true,
 "input_handle": "check-1-in", "output_handle": "check-1-out",
 "audio": {"format": "s16le",
           "input": {"rate": 16000, "channels": 1, "stream": 3},
           "output": {"rate": 48000, "channels": 1}}}
```

(an output-only check — a text to speak — has `"input": null` and no
`input_handle`). `"check": true` is the one sign that the session is a
check: **keep nothing of it** — no recording window, no speaker you
observed, no correction learned — because the voice in it is your
package's, not anyone the identity hears. Session ids are never repeated
within one process ("check-<n>" once per start) and the input stream is
a fresh number per session, as for any session. During a check's session
every host call you make is answered at once: `settings.get` with your
package's declared values, everything else refused with
`VALIDATION_DENY` — treat it as unavailable, never wait on it. A
`failure` you emit during a check is a wrong answer unless it carries
`"retry_may_pass": true` (with a `reason_code`): then it counts as a
timeout, momentary, as for a deadline you missed or memory you could not
have at that moment. `examples/voice-skel` accepts the sign and keeps
nothing of any session, and ships two checks of its own.

## 17a. Credentials: handles, and OAuth

A credential is never a value in your code. A setting of `"type":
"secret"` hands you a HANDLE (`Values.Handle`), the name of an auth
profile the operator configured; you cite it on a call (`AuthProfile`)
and the host's broker injects the value at the wire — a bearer header,
a basic header, the URL path at `sdk.CredentialPlaceholder`, or, for a
profile of scheme `oauth2`, an access token the host refreshes from a
refresh token the operator's own consent yielded. Your code is the
same in every case: cite the handle, read the reply.

An `oauth2` profile is created and connected on the Plugins page (a
tab in the operator's browser, a pasted redirect, or a device code
where the authority allows it). Give your secret setting an `oauth`
hint so that form opens prefilled when the identity asks to connect:

```json
{"key": "google", "type": "secret", "title": "Google account",
 "oauth": {"provider": "google", "services": ["calendar"]}}
```

`provider` is one of the host's templates — google, microsoft, github,
slack — or `custom`; `services` names what you speak to (calendar,
gmail, drive, contacts, mail, files, issues, repo, chat). Whether the
consent is read or modify is the operator's choice on the connect
card; a plugin granted read only has every operation that writes or
executes refused before it reaches you. Two refusals you can read by
name: `NET_AUTH_PROFILE_DISCONNECTED` (the operator must connect the
profile again) and `NET_AUTH_PROFILE_UNAVAILABLE` (a refresh failed
just now; try later). `examples/google-calendar` and
`examples/github-issues` cite such handles.

## 18. Channel adapters: describe, send, receive

A channel adapter is how the identity is reached by a person and reaches
one back: SMS, email, Telegram, Matrix, a chat's direct messages. Its
manifest says `"plugin_family": "channel_adapter"` and declares the
`aii.channel` interface (version 1), and it speaks three methods the
host drives and the identity never sees — `describe`, `send` and
`receive`. The host resolves and the adapter speaks: who may be
written to, who may wake the identity, and what a message becomes are
the host's; the protocol is yours. `examples/twilio-sms` is a webhook
adapter; the contract below is what every adapter keeps.

**describe** takes no arguments and answers
`{"channel": "telegram", "receive": "poll", "budget_seconds": 10}`
(`sdk.ChannelDescription{...}.Value()`):

- `channel` is a token, `[a-z0-9][a-z0-9._-]{0,31}`, because it names
  row keys, log lines and the frame the identity reads outside the
  untrusted sentinel. Prose is not a channel; an adapter that names one
  is installed and carries nothing. Two adapters naming the same channel
  both carry nothing, and a third does not win it.
- `receive` is `poll` (the default: the host loops your blocking
  receive) or `webhook` (§13: the host never calls receive).
- `budget_seconds` is how long `receive` may block before it returns;
  0 takes the host's default of ten, sixty is the ceiling. The host
  reads it to know how long to wait for a receive in flight when it
  stops your listener.
- `acknowledges` (a boolean, `sdk.ChannelDescription.Acknowledges`) says
  you move your position on only through what the host says it
  recorded, below. Say it only if your `receive` input schema admits
  `recorded`.

**receive** (poll adapters) takes no arguments — or, for an adapter
that says `acknowledges`, `recorded` (below) — is called by the host in
a loop, blocks until there is news or the budget is spent, and answers a
JSON array of arrivals, `[]` when nothing came:

```json
[{"id": "12345:678", "from": "12345", "body": "are you there?"}]
```

- `id` is the channel's own stable message id — for Telegram the chat
  and message id, never the update id, since an edit arrives as a new
  update and is not a new message. The host records an arrival once by
  id, so a replayed update is safe.
- Your cursor — Telegram's offset, Matrix's `since` — moves on only once
  the host has what you returned. An adapter that says `acknowledges` is
  handed `recorded` with every receive: the ids of your last answer's
  arrivals the host holds (recorded now, already held, or declined for
  good), an empty list on the first call. Move your cursor past an
  arrival only when its id is in that list — hold the batch in memory
  until it is — and make the cursor durable (kv, `ring4.kv`) when it
  moves. An arrival the host's record refused is left out, so your
  cursor stays at it, the service gives it again, and the host keeps
  each once by `id`. A process killed between your return and the host's
  word revives at the cursor kv holds and is given the same messages;
  a cursor stored before you return loses exactly them. A host that
  starts anew has recorded nothing to tell, so it says `recorded: []`
  and you offer again what you hold. If your describe says
  `acknowledges` and a receive arrives with no `recorded` at all, the
  host is an older one: its next receive is its word that it is
  finished with your last answer, and you may move on then, as adapters
  always did. An adapter that does not say `acknowledges` is never
  handed the field, and keeps that older contract; an arrival the older
  host could not record, or an answer it refused, is in its log
  (`channel.error`) and is not asked for again. Keep every answer one
  the host can take whole — a brokered response is at most 768 KiB by
  default — by bounding the batch you ask for.
- A push adapter has no receive to carry `recorded`, so the host hands it to
  the calls it does drive — each webhook call and each scheduled call — when
  `describe` says `acknowledges`: the ids of your last answer that had
  arrivals, which the host holds (an answer with none changes nothing, so a
  renewal never makes the host forget). A webhook or scheduled answer may carry
  `arrivals`, up to fifty, beside the single `arrival`; declare the schedule in
  `plugin.json` (`schedule`: at most four `{operation, every_seconds}`, ten
  minutes to a week, each a described operation that waits for no operator — the
  host says one that fails, once a day, and never offers it to the model), and
  a Google push as `"scheme": "google-oidc"` with an `email_setting`.
  `examples/gmail` is the adapter these lessons come from.
- Bound your own sends. A contact in the address book is the consent to a
  message, not to a thousand, and an identity in a loop would send until it
  was stopped. Declare two integer settings, `send_limit` (default 20, 1 to
  1000) and `send_window_minutes` (default 60, 1 to 1440), and `ring4.kv`,
  and wrap your send in `sdk.SendWindow{Key: ...}`: `Admit(c)` before you ask
  the service — a send over the limit is refused as `NET_REMOTE_FAILED`, saying
  which limit, where the operator sets it and when the next may go — and
  `Record(c, v)` after a send that went or may have (`Counts(err)`), never one
  the service refused. The window is the host's clock (`_host_now_ms`) over
  times kept in your kv; without either, refuse and say which is missing.
  `examples/telegram`, `examples/twilio-sms` and `examples/gmail` do this.
- `from` is the sender's address as the operator's address book would
  spell it — a chat id, an E.164 number, an email address — never a
  display name. The host matches it against the address book to decide
  whether the sender may wake the identity; a display name is the
  sender's to forge.
- Block; do not spin. A blocking wait costs nothing while nothing
  happens, and it is what the host expects. An empty answer that comes
  back at once meets a one-second floor on the host, but a wait is the
  design. For a WASM guest, the broker's HTTP call bounds the wait: pass
  `timeout_ms` a little under your budget (the broker's own default is
  10 s and its ceiling 60 s).
  The supervised WASM worker's default wall-clock invocation allowance is
  90 s: that maximum broker wait plus 30 s for the rest of the invocation.
  Earlier caller cancellation still wins; successive calls do not extend
  the invocation deadline. Older hosts with a 30 s worker limit need the
  host repair before a plugin can use longer polls; rebuilding a guest alone
  does not change a host's deadline.
- Return within the budget. When the host stops your listener — the
  adapter was uninstalled, its route changed — it asks first and waits
  the budget plus a grace for the receive in flight; only a receive
  that overstays is cancelled, and a cancelled invocation ends the
  process (revived at once; not counted against the plugin).
- One invocation at a time per plugin: while `receive` blocks, `send`
  and `describe` wait behind it, and so does every message the identity
  queued for your channel. Keep the budget at ten unless the protocol
  needs more.

**send** takes `{"address": "...", "body": "..."}` — the address from
the operator's address book, the body the identity wrote — and answers
a receipt. The host never repeats a send whose effect is uncertain, and
it acts on your answer's reason code, never its words, so the code says
what the service accepted:

- **A receipt**: the service took the message. The host marks it
  delivered — an adapter took it, which is not a person reading it.
- **`NET_EFFECT_UNKNOWN`**: the message may have gone — a request
  written whose response was lost (`sdk.EffectUnknown(err)`), a 5xx (a
  server error can follow acceptance), an answer you cannot read. The
  host parks the message with its effect unknown, says so to the
  identity and the operator, never retries it and never tries the
  person's next channel — the one path that could deliver a message
  twice.
- **`NET_REMOTE_FAILED`**: the service accepted nothing from this send —
  it refused the request (a 4xx: a rate limit, a refused credential),
  or never received it (the receipt's effect is `not-performed`). The
  host tries the person's next channel, counts the attempt, tries again
  at the next turn end, and parks the row after eight with your last
  answer on record.
- **`OPERATION_ARGUMENT_INVALID`**: nothing was sent, and nothing will
  be on this channel — the address cannot take a message, or the body
  is one the protocol cannot carry. The host tries the person's next
  channel and, when none takes it, parks the row after that walk:
  retrying cannot fix an address.
- **Anything else is the host's to read.** A code the broker mints only
  when no request of the call was written — its denial, relayed as you
  received it — walks on as not sent, by the broker's own list.
  Everything the host cannot prove unsent is unknown: an untyped
  failure, a result it refused, an invocation that died, and the
  broker's `NET_REMOTE_OUTCOME_FAILED` passed through — it names a
  request never written and a 5xx after one alike, and only you hold
  the receipt's effect and the status that tell them apart.

Two rules keep those codes true. `NET_REMOTE_FAILED` means the service
accepted nothing from this send — a verdict on the service, never
"something failed". And a send that writes more than once — a long
message in parts — answers `NET_EFFECT_UNKNOWN` for any failure after
its first write: the first part may already be with the person.

None of the three operations may declare `OperatorConfirms` (§10). The
host drives them, and an operation waiting for the operator neither
carries a message nor answers the host, so the host refuses the
activation of an adapter that declares it on any. `aiisdk package` and
`aiisdk test` do not check this; the host does, when it activates.

A service that answers 429 or 503 may say when to ask again: in a
header, which the host reads for you as `res.RetryAfterS` (§11), or in
its own body (Telegram's `parameters.retry_after`). Whether that answer
means the service took nothing, and whether to wait and send again, are
yours to decide from what the service documents, inside the
invocation's deadline and its cancellation; the host does not wait on
the hint, and a counted row is tried again at the next turn end,
whenever that comes. A failure carries no field for the wait, so say it
in the answer's words, where the identity and the operator read it. A
hint never makes a send whose effect is unknown safe to repeat.

**What the host does, so you never have to.** The address book —
name, channel, address and a per-contact wake grant — is the operator's
config. An unknown sender never wakes the identity and a known one only
with the grant; either way the body is wrapped as untrusted under a
frame that never carries the sender's address outside the sentinel, and
there is no auto-reply: the identity answers by naming a person to
`send`, and delivery resolves the address at that moment in the
operator's order. Under SAFE nothing leaves. The identity reads what
became of each message it sent in its next turn, and the operator sees
arrivals and the outbox on the page.

**The Telegram adapter, `examples/telegram`.** `describe` answers
`telegram`, `poll`, budget ten, `acknowledges`. `receive` calls `getUpdates` with
`timeout=9`, `limit=1` and the offset it holds, `timeout_ms` 9500, and
maps each message to `{id: "<chat>:<message_id>", from: "<chat>", body:
text}` — the caption, or `[photo]` and its kin, when there is no text.
Telegram forgets an update once it is asked for a later offset, so the
offset moves past an update, and is written to kv when kv is granted,
only once the host says (`recorded`) that it holds the arrival the
update became; an update that made no arrival moves on with the ones
before it. One update a poll keeps every answer
far under the broker's ceiling; a hundred long messages, Telegram's
default batch, can cross it and stop the channel. `send` refuses a body
outside Telegram's 1 to 4096 characters before anything leaves, calls
`sendMessage` once, and answers `chat:message_id` as the receipt; a 403,
or a 400 about the chat (`chat not found`, or a group that moved,
`parameters.migrate_to_chat_id`), as `OPERATION_ARGUMENT_INVALID`; a
429, with the wait Telegram named, and any other 4xx or `ok:false` as
`NET_REMOTE_FAILED`; a 5xx, a lost response or a success it cannot read
as `NET_EFFECT_UNKNOWN`. Its tests drive each of those answers, and a
kill between a receive's return and the host's record, against a
stand-in host over the native lane. Telegram takes the bot token in the URL
path, so the URLs name `sdk.CredentialPlaceholder` (`{credential}`)
and the token is a credential handle behind an auth profile of scheme
`path` pinned to `api.telegram.org:443`: the broker substitutes it when
it dials, keeps the placeholder in every record, scrubs the value from
what comes back, and the plugin never sees it. The operator adds a
contact with channel `telegram`, the chat id as the address, and wake.

## 19. A source of vectors: the meaning layer's interface

The identity's memory has a meaning layer: a recall finds memories that
are near a cue in a model's sense, by comparing vectors. A plugin can be
what makes those vectors, on the operator's own machine. Its manifest
says `"plugin_family": "provider_bridge"` and declares the
`aii.embeddings` interface (version 1) with one method, `embed`, which
the host drives and the identity never sees.

**embed** takes `{"kind": "memory", "text": "…"}` or
`{"kind": "cue", "text": "…"}`: one text of at most 8,000 characters. It
answers `{"vector": [ … ]}` and nothing else: exactly the declared
number of finite numbers, not all zero. Another member, text where a
number goes, a vector of another length: the host refuses the reply
whole, and nothing in it reaches the identity. `kind` is there because
a model may read a cue and a memory differently (a prefix on one of
them); a model that reads them alike ignores it.

What such a plugin is handed is the most private text an identity has.
So the host admits the package only if all of the following holds, and
`aiisdk package` refuses what the host would (reading `plugin.json`
alone refuses what that file can show, in every command that reads it):

- `aii.embeddings` is its only interface, and it is under
  `provider_bridge`;
- its descriptor does not ask for the operator's confirmation
  (`OperatorConfirms`). The host calls `embed` for every memory and
  every cue, and a card in place of a vector answers nothing;
- every variant is `native_t3_component` (section 15). Only a native
  child is handed the model the package declares, and the host names
  the vectors by that model's hash;
- `capability_envelope` is `[]`, and so is every variant's
  `variant_capabilities`; it declares no settings, webhooks, schedule
  or subscriptions. A source computes. It has nowhere to send what it
  is handed and nothing to keep it in;
- `aiios_min_version` is at least `0.1.15`. An earlier host does not
  know the interface and would offer `embed` to the identity as a tool;
- it carries a declaration of its vectors, named in `plugin.json` as
  `"embeddings_file": "embeddings.json"` and packaged as
  `embeddings.json`:

```json
{"dimension": 384, "form": 1, "floor": 0.3, "model": "model.onnx",
 "probe_bound": 0.95,
 "probes": [{"kind": "cue", "text": "…", "vector": [ … ]},
            {"kind": "memory", "text": "…", "vector": [ … ]}]}
```

`dimension` is the length of every vector, 1 to 4096. `form` numbers
everything besides the model that makes a vector what it is: what is
prefixed to a cue, where a long text is cut, which token's vector is
taken, how it is scaled. Change any of them and you change the number.
`model` names one entry of `models`, one that is always present (no
`when`). The host names every vector it keeps by the package's id, the
first twelve hex digits of that model's sha256 and the form, so a new
version with the same model and form keeps the identity's vectors, and
a new model or form has them made again. `floor` is the similarity
under which a recall shows no meaning match, above 0 and at most 1, on
your model's own scale. Measure it: models spread their similarities
differently, and a floor taken from another model hides every match or
none.

`probes` are texts with the vectors YOUR build returns for them: 1 to
32, at least one of each kind, none longer than a text the host hands
over, and none of white space only, since the host never hands a source
such a text. A probe's vector must have a length: entries that are each
finite and too large to square are refused, as a vector of zeros is. The
file itself must be valid UTF-8, and an escape in it must name a
character (half of a surrogate pair does not). The host asks for every one of them each time the plugin starts,
before it hands over a single memory, and compares each answer with the
signed vector by cosine. `probe_bound` is the least agreement you ask
of your own installs, 0.9 to 1, yours to set for your model and the
processors you ship for. One probe under it and the source is not used
until it starts again. Probes catch an install that computes something
other than your build did: a missing prefix, another pooling, the wrong
model file. They run at every start, so keep them few and short.

The file is packaged exactly as you wrote it. The kit's reader and the
host's agree on every case of `vectors/embeddings.json`.

## 20. Your checks: validation.json

A host proves that your plugin works on the machine in front of it the
way you say it does: with checks you write. Each is an input you send,
on a lane the host already talks to your plugin on, and the answer it
must bring back within a bound. The host runs them after your plugin
has reported ready and answered healthy, before anything else reaches
it, at install and at every start. Name the file in `plugin.json` as
`"validation_file": "validation.json"`; it is packaged as
`validation.json` exactly as you wrote it.

```json
{"checks": [
  {"name": "embeds probe 3", "sets": ["*"], "lane": "embeddings",
   "input": {"kind": {"$ref": "embeddings.json#/probes/3/kind"},
             "text": {"$ref": "embeddings.json#/probes/3/text"}},
   "expect": {"vector_cosine_at_least": {
     "reference": {"$ref": "embeddings.json#/probes/3/vector"},
     "min": {"$ref": "embeddings.json#/probe_bound"}}},
   "within_ms": 5000},
  {"name": "finds the harbor", "sets": ["*"], "lane": "operation",
   "call": {"operation": "search", "arguments": {"q": "harbor"}},
   "expect": {"result_contains": ["harbor"]},
   "within_ms": 2000}
]}
```

- **name** is yours, up to 120 characters: when a check fails, the
  operator's Plugins page names it in your words.
- **sets** is `["*"]`, or the `variant_id`s of the component sets the
  check is for; a check names no set the package does not declare.
- **lane** is `operation` — `call` names one of your operations and its
  arguments, and your descriptor must declare it a read
  (`read.internal` or `read.external`) that waits for no operator —
  `embeddings`, for a source of vectors (section 19): `input` is a
  `kind` and a `text`, sent as the host sends a memory or a cue — or
  `session`, for a resident speech engine (section 17; below). A host
  shows a check of a lane it does not run as "not run by this host" and
  holds nothing against your set; the packer refuses one, since no host
  runs it.
- **expect** is one of `result_contains` (a string, or a list that must
  all appear in the result), `json` (`path`, a JSON pointer into the
  result, and either `equals` a value or `at_least` a number), or, on
  the embeddings lane, `vector_cosine_at_least` (the full cosine of the
  vector you answer with a `reference`, at least `min`).
- **within_ms** is the check's own bound, 1 to 600,000, measured from
  send to reply. It is not the call's deadline: the call keeps the time
  every call of its lane has, so a slow answer is a slow answer and
  never a stopped plugin.
- **$ref** takes any value — an input, a reference, a bound, an
  argument — from another file of the same package by a JSON pointer:
  `"embeddings.json#/probes/3/text"`. It is read once, when the package
  is read. A `$ref` that leaves the package, names a file it does not
  carry or points at nothing fails that check on a host; the packer
  refuses it.

**The session lane** is a resident engine's (`"plugin_family":
"voice_interface"`, native sets only). Its `input` is exactly one of
`"audio"` — the path, inside your plugin's directory, of a recording the
packer carries into the package: a 16-bit PCM WAV, one or two channels
at 8 to 192 kHz, 0.1 to 30 seconds, at most 8 MiB of recordings a
package — or `"text"`, 1 to 1,000 characters your engine speaks. Its
`expect` is `events`, at least one of:

- `order`: up to 16 event types that must appear in that order among
  your critical events (others may come between); never
  `vad_probability` or `transcript_partial` (you may shed them), never a
  terminal word, and on a text's check never an input event;
- `transcript_contains` (a recording's check): up to 16 phrases your
  final transcripts must hold as consecutive words, once both are
  normalised — lowercase, punctuation dropped, apostrophe contractions
  expanded, a number's digits spelled one by one ("17" is "one seven");
- `audio_at_least` (a text's check): `{"ms": N, "rms_dbfs": X}`, the
  reply's length in your engine's samples and its level (the root mean
  square of its samples in decibels of full scale).

```json
{"name": "hears its tail", "sets": ["*"], "lane": "session",
 "input": {"audio": "checks/tone-16k.wav"},
 "expect": {"events": {"order": ["session_ready", "transcript_final", "input_finished"],
                       "transcript_contains": ["the tail, finalized"]}},
 "within_ms": 10000}
```

The host opens the session itself (section 17), waits for
`session_ready`, sends the recording in real time on its own sample
clock and then `finish_input` at its exclusive end — your answer is
complete at `input_finished` — or sends one `synthesize`, complete at its
`synthesis_end` with its stream's END; then it aborts the session and
waits for your word that it ended. Only critical events up to the
complete answer are judged; slow is never wrong, only late. A refused
control, a `failure`, your engine cancelling the check's own synthesis
or ending the session first is a wrong answer — a failure with
`"retry_may_pass": true` is a timeout instead — and a session you never
end after the abort is a timeout that stops your engine. The words a
check hears are its own: they reach no page, no record and no turn.
`within_ms` counts from the open and must be longer than the recording.

**While a check runs, the host gives your plugin nothing of the
identity's**: no memory, no interactions, no storage or files of yours,
no provider, no network, and every write is refused (reason code
`VALIDATION_DENY`). `settings.get` answers the values your package
declares, never the operator's. A check exercises your code, never an
identity's record.

**What an ending means on a host.** Every check passes: the set is used,
and its card says "its publisher's checks passed on this machine" and
when — your word that it works there, not a proof that it is correct.
A wrong answer, or a value that does not resolve: that set is not used
on that machine, the next set it can run is, and with something already
serving it keeps serving; it is tried again only when your package, the
set or the host's build changes, or the operator presses Try again. A
right answer past `within_ms`: the set serves, its card says its checks
have not yet passed, and they run again at its next start; at three
separate starts within 24 hours it counts as a wrong answer. A call that
could not run at all is not held against the set. A package with no
checks is used on its readiness alone, and the card says so.

A source of vectors that carries no `validation.json` has its probes run
as its checks, as they always were (section 19). One that carries it is
checked by it alone: take the probes into it by `$ref`, as above.

**The packer refuses** a check of an operation that is not a read, of
one that waits for the operator, of an operation or set you do not
declare, the embeddings lane on a package that is no source of vectors,
the session lane on one that is no resident engine or on a set that is
not native, a recording the host cannot send, any lane but the three
above, and a value that cannot resolve. It carries each recording a
session check names into the package, where the host verifies it like
any member. A check's
arguments are not held to your operation's input schema by the packer;
a host fails such a check alone.

**`aiisdk test` runs them.** After your cases, each check is sent and
judged as the host judges it, and each is reported: its name, how it
ended (passed, wrong, timed_out, unresolved, unanswered), the cosine
reached on the embeddings lane, and the time it took; the first failure
is named, and the run passes only if every check does. A native plugin
(`-native`, or a plugin with no wasm variant) is run as the host runs
it: the carrier your variant for this machine names is started with
exactly the environment the host gives it — `SEV_PLUGIN_SOCKET`,
`SEV_PLUGIN_ID`, and `AII_MODELS_DIR` and `AII_RUNTIME_ROOT` from
`-models-dir` and `-runtime-root` (or the same variables set for the
command) — its ready line, naming the kit that built it (`sdk=`), is
awaited, and its checks run. Nothing is fetched, and your carrier's own
warm probe is its readiness, not a check. A resident engine is started
as the host starts one, with its session lane and its audio pair, and
its session checks run as above, `"check": true` in every open; one
difference is said, not hidden: a host converts your recording to the
input format your engine answers, and this kit does not, so a recording
at another rate is reported "not run here" with the rate your engine
answered, and the run ends INCOMPLETE (exit 3), never as a pass — record
the check at that rate.

```sh
aiisdk test -native -models-dir ./models -runtime-root ./runtime
```

The kit's reader, its JSON pointer and its cosine agree with the host's
on every case of `vectors/validation_checks.json`. A host older than
the one that reads `validation.json` ignores the file; if your plugin
must not run without its checks, name that host in `aiios_min_version`.

## Where to look next

- `pkg/aiiosdk` package doc — the guest library's contract: arguments,
  results, host calls, why `init()`.
- `examples/` — fourteen plugins, each with a README, from the smallest guest
  to a resident speech engine.
- `acceptance/` — the end-to-end proof of everything above, for
  maintainers with a host checkout (`CONTRIBUTING.md`).
