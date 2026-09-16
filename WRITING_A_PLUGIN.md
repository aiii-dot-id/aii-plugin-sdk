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
`test`, `devcert`, `sign`, `revoke`, `publish`, `runtime-pack`. `aiisdk
help` is the map.

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
		// Read arguments with the reflection-free walker — NOT
		// json.Unmarshal into a struct, which traps a wasm-unknown guest.
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
  operator confirms it. So a call the identity made with NO arguments still
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

Whatever operations you `Handle`, mirror them in `plugin.json`'s
interface — the packager reads your descriptors, and the host routes one
invokable method per declared operation.

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
 "params":{"operation":"greet","arguments":{"name":"Nova"}}}
```
→
```json
{"jsonrpc":"2.0","id":1,"result":
 {"status":"succeeded","operation_result":{"greeting":"Hello, Nova!"}}}
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
lives only under `.keys/` (0600, gitignored); the kit never prints it.

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
file-write and the OS credential stores, so Metal is reachable; the
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

**The child is contained on every desktop.** The mechanism differs by
platform; the property does not: no network of its own, nothing
writable beyond what the host hands it, and it dies with its supervisor.

| | native T3 containment |
|---|---|
| Linux | bubblewrap: read-only root, no writable path, `--unshare-net` |
| macOS | Seatbelt via `sandbox-exec`: no network, read-only filesystem |
| Windows | an AppContainer with no capabilities — no network — inside a job object: dies with the supervisor, no breakaway, UI-restricted; it reads what the host grants it and writes only its own container folder |
| Android / iOS | unreachable: mobile T3 is in-process bundled, and iOS forbids exec |

**Write your plugin as though the wall is there**: on the three desktops
it is. But do not rely on it to contain a bug: a wall bounds what a child
can reach, not what it does with what it reaches — and on macOS the
child can still read most of what its user can read (only the OS
credential stores are denied), so what you load, cache and log is still
yours to handle correctly.

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
loaded, the accelerator, one bounded inference — before it sends work;
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
host holds a success to your output schema, and a denial is classified
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

Capabilities are requested per operation **in the host's own names**:
`ring4.kv`, `ring4.memory`, `model.embeddings`, `net.outbound:host:port`,
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
value into that one request and nothing else.

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

## 12. Files: your own directory, and the operator's folders

`sdk.Files` gives you two kinds of place. Your **private directory**
needs no grant: declare `fs.private`, and `sdk.Files.Write(sdk.PrivateRoot,
"index.tsv", data)` lands under the identity's data directory in a
folder of your own, which lives as long as your release does at a
publisher-proven tier and dies with the activation below it, like RING4.
A **granted root** is a folder the operator chose for you by name —
`plugins.grants.<id>.roots: [{name: "docs", path: "/…", write: false}]`
— behind `fs.roots` and a publisher-proven tier; you read it as
`sdk.Files.ReadAll("docs", "notes/today.md", limit)`.

The host holds every path to one discipline: relative to its root, no
traversal, no symlink followed anywhere on the way, nothing you write
ever executable, reads in pieces under the response ceiling and writes
in pieces under the request ceiling, and a files ceiling from the
resource envelope. A root that contains the identity's own files — the
ledger, the key, the store, the config, a credential file — is refused
whole, so an operator cannot hand you their home directory by mistake.

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
directory of the run, and `-grant root:docs=sample` grants a folder
read-only (`:rw` to write). `examples/document-ingest` is the plugin
these lessons come from: it ingests a file from a granted folder into
RING4, keeps a private index, and its cases prove the refusals.

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

**The runtime**, when your engine is more than one executable: pack its
interpreter, libraries and code — never model data — with
`aiisdk runtime-pack -dir <tree> -o engine-runtime.tar.gz`,
publish the archive, and declare it per native variant:

```json
"runtimes": [{"variant_id": "windows-x86_64-native", "url": "https://…/engine-runtime.tar.gz",
  "sha256": "<the archive's, as runtime-pack prints it>", "size": 300000000,
  "installed_bytes": 900000000, "files": 15000,
  "inventory_sha256": "<the inventory member's, as runtime-pack prints it>"}]
```

Every number is what `runtime-pack` printed for THIS archive: the tar's
digest and size, never a zip's, and the inventory member's digest, never
your engine's own profile file's. The host fetches the archive at
activation — never after your process starts — verifies every file
against the inventory the archive leads with, places your carrier at
the root of the tree (beside `python/`, `engine/` and whatever else you
packed: your executable's parent IS the runtime root, and the host adds
no other file to it), and runs it from there with `AII_RUNTIME_ROOT`
beside `AII_MODELS_DIR`; the tree is read-only under the containment
and pinned for the activation's life. Resolve your runtime adjacent to
your executable; the environment name is informational. Nothing under
`AII_MODELS_DIR` is ever executed. The host's ceilings (1 GiB installed, 32,768 files,
512 MiB per file by default; the operator's to adjust) refuse a
dependency-complete development stack, so pack the closure your engine
loads, not everything it was built with. A runtime that genuinely needs
more is packed under an explicit budget (`-max-installed-bytes 8G
-max-files 20000 -max-file-bytes 1G -max-compressed-bytes 4G`): the
budget is your declared requirement, never permission, and the report
then names as `requires_operator_ceilings` the settings the operator
must raise on the host's Plugins page before the activation is
admitted — state them in your README; `aiisdk package` repeats them.
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
`__init__.py`, `__pycache__`, `libstdc++.so.6`, `.dylibs`,
`Lorem ipsum.txt` and `script (dev).tmpl` are all admitted.

**Real readiness.** A variant that declares a profile must report
readiness, not merely spawn: load the models, bring the accelerator up,
run one bounded inference, then

```go
p.ServeReady("child-ready", sdk.ReadyReport{ModelsLoaded: 4, Accelerator: "mlx", ProbeMS: 38})
```

The host refuses a child that spawned without the three fields. On
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
  for anything past that — never replay on unknown.

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

**receive** (poll adapters) takes no arguments, is called by the host in
a loop, blocks until there is news or the budget is spent, and answers a
JSON array of arrivals, `[]` when nothing came:

```json
[{"id": "12345:678", "from": "12345", "body": "are you there?"}]
```

- `id` is the channel's own stable message id — for Telegram the chat
  and message id, never the update id, since an edit arrives as a new
  update and is not a new message. The host records an arrival once by
  id, so a replayed update is safe. Persist your offset in kv
  (`ring4.kv`) before you return, so a revived process replays only what
  the host already has.
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
a receipt. Its effect must be honest:

- A request that was written and whose response was lost is
  `NET_EFFECT_UNKNOWN` (`sdk.EffectUnknown(err)` on the broker's HTTP
  error). The host parks the message with its effect unknown, never
  retries it and never tries the person's next channel — the one path
  that could deliver a message twice.
- An address the protocol cannot take is `OPERATION_ARGUMENT_INVALID`.
  The host parks the message at once; retrying cannot fix an address.
- Any other failure is a counted refusal: the host walks the operator's
  next channel for that person, tries again at the next turn end, and
  parks the row after eight refusals with your last answer on record.

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
`telegram`, `poll`, budget ten. `receive` calls `getUpdates` with
`timeout=9` and the offset it holds (copied to kv when granted),
`timeout_ms` 9500, maps each message to `{id: "<chat>:<message_id>",
from: "<chat>", body: text}`, writes the next offset, and returns the
list. `send` calls `sendMessage` with `chat_id` = address and `text` =
body, answering `chat:message_id` as the receipt, `NET_EFFECT_UNKNOWN`
when the response is lost, and `OPERATION_ARGUMENT_INVALID` when
Telegram refuses the chat. Telegram takes the bot token in the URL
path, so the URLs name `sdk.CredentialPlaceholder` (`{credential}`)
and the token is a credential handle behind an auth profile of scheme
`path` pinned to `api.telegram.org:443`: the broker substitutes it when
it dials, keeps the placeholder in every record, scrubs the value from
what comes back, and the plugin never sees it. The operator adds a
contact with channel `telegram`, the chat id as the address, and wake.

## Where to look next

- `pkg/aiiosdk` package doc — the guest library's contract: arguments,
  results, host calls, why `init()`.
- `examples/` — twelve plugins, each with a README, from the smallest guest
  to a resident speech engine.
- `acceptance/` — the end-to-end proof of everything above, for
  maintainers with a host checkout.
