# Hatch

A standalone Go CLI for creating and inspecting experimental projects on macOS and Linux.

## Build and use

Requires Go 1.24 or newer to build; the resulting binary needs no Go runtime or SQLite installation.

```sh
go build -o hatch .
./hatch new project-alpha
./hatch info project-alpha
./hatch --help
./hatch new --help
```

Names contain lowercase ASCII letters or digits, separated by single hyphens (for example, `project-alpha` or `test-2`). Names are unique among tracked projects.

Creation makes an empty `~/experiments/YYYY-MM-DD-<name>` directory with active status. The date is the local calendar date at creation and never changes. SQLite tracking lives at `~/.local/share/hatch/hatch.db`; stored locations are absolute. A monotonic registry sequence preserves creation order for a future list command. Existing destinations, including files and dangling symlinks, are never overwritten or imported.

`info` prints name, creation date, status, and full location. Missing files produce a warning without changing status. Unknown names and command errors exit with status 1; successful commands and help exit with status 0. Inspection on a fresh installation creates nothing.

This first release implements only `new` and `info`. TOML configuration, XDG overrides, list, status changes, promotion, and removal are intentionally deferred. Defaults above apply even when XDG variables are set.

## Creation safety and recovery

Commands accessing an existing registry share an OS advisory lock (`mutation.lock` beside the registry). SQLite uses fully synchronous transactions and a unique name constraint. A durable pending-creation record precedes directory creation. After syncing the directory and its parent, Hatch records the directory's device/inode identity. Registry insertion and clearing the pending record commit in one transaction.

On a subsequent invocation:

- Intent with no destination is cleared; creation can be retried.
- A durably identified, unchanged directory completes registration with the original date.
- A committed project remains tracked, even if its files later disappear.
- An existing destination without recorded ownership, a changed directory identity, or a missing previously identified directory is uncertain. Hatch preserves files and pending evidence, reports the path, and blocks operations requiring recovery. This includes `info`; help remains available.

In particular, interruption between making the directory and recording its identity deliberately requires manual investigation—even if the directory is empty. Hatch never guesses ownership or deletes files during recovery. There is no automatic repair command in this release. Preserve the reported path and the registry before investigating; deleting the registry or pending evidence is not a safe generic repair.

Coordination assumes a local filesystem supporting SQLite locking, advisory locks, and directory syncing. It coordinates Hatch processes, not unrelated programs editing paths concurrently. Device/inode identity is evidence for ordinary restart recovery, not protection against malicious filesystem manipulation or inode reuse.

## Tests

```sh
go vet ./...
go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/hatch-linux .
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o /tmp/hatch-macos .
```

Tests build a CLI binary, invoke separate processes with disposable homes, and assert output, exit statuses, and files—not private database layout. The `hatchtest` build tag enables internal deterministic clock and abrupt-exit controls; these controls are absent from ordinary builds. Tests cover collisions, local-date persistence, concurrent creation, and interruption before/after directory creation and during/after registry completion. Run the suite on both macOS and Linux to exercise each platform's actual filesystem behavior.
