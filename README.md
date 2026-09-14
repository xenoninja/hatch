# Hatch

A standalone Go CLI for creating, listing, and inspecting experimental projects on macOS and Linux.

## Build and use

Requires Go 1.24 or newer to build; the resulting binary needs no Go runtime or SQLite installation.

```sh
go build -o hatch .
./hatch new project-alpha
./hatch info project-alpha
./hatch list
./hatch list --help
./hatch --help
./hatch new --help
```

Names contain lowercase ASCII letters or digits, separated by single hyphens (for example, `project-alpha` or `test-2`). Names are unique among tracked projects.

Creation makes an empty `~/experiments/YYYY-MM-DD-<name>` directory with active status. The date is the local calendar date at creation and never changes. SQLite tracking lives at `~/.local/share/hatch/hatch.db`; stored locations are absolute. A monotonic registry sequence preserves creation order for `list`. Existing destinations, including files and dangling symlinks, are never overwritten or imported.

`info` prints name, creation date, status, and full location. Missing files produce a warning without changing status. Unknown names and command errors exit with status 1; successful commands and help exit with status 0. Inspection on a fresh installation creates nothing.

`list` prints a table of every tracked project's name, creation date, and status:

```text
NAME           CREATED     STATUS
project-beta   2026-09-14  active
project-alpha  2026-09-14  active
```

Rows use recorded creation order, newest first—not alphabetical or calendar-date order. For creations on the same date, the later registry sequence comes first; the unique sequence makes ordering deterministic even if the clock changes. No folders are scanned or imported, and no records are filtered by status or location availability. Missing or non-directory locations produce warnings on stderr with their stored paths; records and lifecycle statuses are unchanged. An absent or empty registry prints `No experimental projects tracked.` and exits successfully without initializing storage. Existing pending operations still undergo the shared recovery rules below.

This release implements `new`, `info`, and `list`. Status changes, promotion, and removal are intentionally deferred.

## Configuration and storage

Hatch reads `$XDG_CONFIG_HOME/hatch/config.toml`, defaulting to `~/.config/hatch/config.toml`. Create this file manually to override the experiments directory:

```toml
experiments_dir = "~/work/experiments"
```

`experiments_dir` accepts an absolute path or a path starting with `~/` (expanded using your home directory). Other relative paths, empty values, non-string values, malformed TOML, and paths whose existing components are not usable directories produce errors rather than falling back. Missing configuration or an omitted key uses `~/experiments`. Missing experiments directories are allowed and created only when needed by `new`.

The registry and its lock live under `$XDG_DATA_HOME/hatch/`, defaulting to `~/.local/share/hatch/`. XDG homes must be absolute; unset, empty, or relative values use their respective defaults. XDG values are not shell-expanded by Hatch. Different data homes have independent registries; changing the data home does not copy or discover old records.

Changing `experiments_dir` affects only subsequently created projects. Existing projects and interrupted creations retain their stored absolute locations; no files are moved. Inspection still reconciles pending operations in an existing registry, even after configuration changes. Invalid configuration fails before accessing storage; help remains available.

Hatch never creates the configuration file or configuration directories. Help and inspection against an absent registry do not create data or experiments directories.

## Creation safety and recovery

Commands accessing an existing registry share an OS advisory lock (`mutation.lock` beside the registry). SQLite uses fully synchronous transactions and a unique name constraint. A durable pending-creation record precedes directory creation. After syncing the directory and its parent, Hatch records the directory's device/inode identity. Registry insertion and clearing the pending record commit in one transaction.

On a subsequent invocation:

- Intent with no destination is cleared; creation can be retried.
- A durably identified, unchanged directory completes registration with the original date.
- A committed project remains tracked, even if its files later disappear.
- An existing destination without recorded ownership, a changed directory identity, or a missing previously identified directory is uncertain. Hatch preserves files and pending evidence, reports the path, and blocks operations requiring recovery. This includes `info` and `list`; help remains available.

In particular, interruption between making the directory and recording its identity deliberately requires manual investigation—even if the directory is empty. Hatch never guesses ownership or deletes files during recovery. There is no automatic repair command in this release. Preserve the reported path and the registry before investigating; deleting the registry or pending evidence is not a safe generic repair.

Coordination assumes a local filesystem supporting SQLite locking, advisory locks, and directory syncing. It coordinates Hatch processes, not unrelated programs editing paths concurrently. Device/inode identity is evidence for ordinary restart recovery, not protection against malicious filesystem manipulation or inode reuse.

## Tests

```sh
go vet ./...
go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/hatch-linux .
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o /tmp/hatch-macos .
```

Tests build a CLI binary, invoke separate processes with disposable homes, and assert output, exit statuses, and files—not private database layout. The `hatchtest` build tag enables internal deterministic clock and abrupt-exit controls; these controls are absent from ordinary builds. Tests cover list ordering (including same-date creations and clock changes), empty lists, missing locations, list recovery, configuration defaults, TOML validation, XDG isolation, home expansion, retained locations and recovery after configuration changes, collisions, local-date persistence, concurrent creation, and interruption before/after directory creation and during/after registry completion. Run the suite on both macOS and Linux to exercise each platform's actual filesystem behavior.
