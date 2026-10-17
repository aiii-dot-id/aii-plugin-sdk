# compat

The plugin AII OS's compatibility suite runs. The host builds it from a
pinned commit of this kit, installs it unsigned (T0) and dev-signed at
0.1.0 and 0.2.0 (T1), and holds what the kit's memory and interaction
clients say to what its own broker does — the grant, the record's
lifetime, a correction, the history a plugin may read, SAFE, a call the
host stops, a restart and an update (`aii-os`
`internal/app/sdk_compat_test.go`, `test/run_sdk_compat.sh`).

| operation | does | declares |
|---|---|---|
| `remember` | records a text; `supersedes` corrects a memory it holds | `write.local`, `ring4.memory` |
| `recall` | searches the record, or reads one memory by `id` | `read.internal`, `ring4.memory` |
| `interactions` | one page of the operator-visible history; page with `next_cursor` | `read.internal`, `interaction.read` |
| `overreach` | declares a read and tries to write: the host refuses it | `read.internal`, `ring4.memory` |
| `spin` | remembers, then computes until the host stops the call | `write.local`, `ring4.memory` |

Every operation returns a refusal as its own result: a host denial is
denied with the host's reason code, a failed host operation fails with
its own. The operator grants `memory` and `interactions` in
`plugins.grants`.

    aiisdk build && aiisdk test

The cases run with no grant, so every operation meets its wall and says
so; `spin` is refused before it spins. The stand-in host serves no
interaction history, and says that by its own code. The granted paths
are the host suite's to prove, against the real broker.
