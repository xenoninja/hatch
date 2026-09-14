# Hatch

A standalone Go CLI for creating, listing, inspecting, classifying, and promoting experimental projects on macOS and Linux.

## Build and use

Requires Go 1.24 or newer to build; the resulting binary needs no Go runtime or SQLite installation.

```sh
go build -o hatch .
./hatch new project-alpha
./hatch info project-alpha
./hatch status project-alpha completed
./hatch status --help
./hatch promote project-alpha ~/work/project-alpha  # ~/work must already exist
./hatch promote --help
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

## Lifecycle status

`hatch status <name> <status>` reclassifies an existing experimental project and prints its persisted information:

- `active`: ongoing exploration; the initial status.
- `completed`: experimental work is finished and retained in place.
- `abandoned`: work has been set aside.
- `promoted`: relocated outside the experiments directory through promotion; terminal and still tracked. Only promotion may set this status. Promoted projects cannot be reclassified, including to promoted again.

Switch freely among active, completed, and abandoned. Repeating the current non-promoted status succeeds. Status changes update only the registry, preserving identity, immutable creation date, current location, creation order, and all files. Missing or non-directory locations warn without preventing reclassification or introducing another status. Unknown names, invalid statuses, and direct assignment of promoted fail with nonzero errors.

Status changes use the shared mutation lock, pending-recovery guard, and a SQLite transaction. An interruption before commit leaves the previous status; after commit the new status persists. No filesystem move or pending status operation is needed.

## Promotion

`hatch promote <name> <target-path>` moves an active, completed, or abandoned experimental project to an external destination. It preserves all existing contents without scaffolding changes, the name, creation date, and list order. `info` then reports terminal status `promoted` and the full new location; `list` still includes the project. Its name stays reserved, and subsequent promotion or status changes fail—even if the promoted files later disappear. Hatch does not track subsequent lifecycle changes at the destination.

The target is the **exact final location**, not a containing directory. Absolute paths and paths relative to the working directory are accepted; the stored destination is absolute with parent symlinks resolved. The parent must already exist and the final target must be absent, including dangling symlinks. Hatch never creates missing parents, merges directories, or overwrites destinations. Missing, non-directory, or symlink source projects fail clearly. Destinations inside the current experiments directory or the source project are rejected, including symlink aliases and sources retained from an older configuration.

Promotion uses an atomic no-replace rename on macOS and Linux. **Cross-filesystem moves are unsupported**: there is no copy/delete fallback, and failure leaves source files and committed metadata unchanged. Choose a destination on the source filesystem. Filesystems without support for exclusive rename fail rather than falling back to an unsafe move.

This release implements `new`, `info`, `list`, `status`, and `promote`. Removal is intentionally deferred.

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
- An existing destination without recorded ownership, a changed directory identity, or a missing previously identified directory is uncertain. Hatch preserves files and pending evidence, reports the path, and blocks operations requiring recovery. This includes `info`, `list`, and `status`; help remains available.

In particular, interruption between making the directory and recording its identity deliberately requires manual investigation—even if the directory is empty. Hatch never guesses ownership or deletes files during recovery. There is no automatic repair command in this release. Preserve the reported path and the registry before investigating; deleting the registry or pending evidence is not a safe generic repair.

Coordination assumes a local filesystem supporting SQLite locking, advisory locks, and directory syncing. It coordinates Hatch processes, not unrelated programs editing paths concurrently. Device/inode identity is evidence for ordinary restart recovery, not protection against malicious filesystem manipulation or inode reuse.

## Promotion safety and recovery

Promotion holds the same registry lock as creation, status changes, inspection, and recovery. Before moving anything it durably records the project name, source, exact destination, and source device/inode identity. After the rename, both parent directories are synced before the status/location update and intent deletion commit together.

On the next registry command, even with changed configuration:

- The original identified source exists and the destination is absent: clear the unexecuted intent, retaining the original status/location. Promotion can be retried.
- The source is absent and the destination has the recorded directory identity: sync the parent directories and complete promotion.
- Both paths exist, both are missing, either identity changed, or a path cannot be inspected reliably: preserve files and pending evidence, report both paths, and block registry commands (`new`, `info`, `list`, `status`, and `promote`). Help remains available.

As with creation recovery, ambiguous states require manual investigation; do not delete registry evidence as a generic repair. The shared lock coordinates Hatch processes using the same registry, not unrelated filesystem edits or independent data homes. Exclusive rename also prevents a destination that appears at move time from being overwritten.

## Tests

```sh
go vet ./...
go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/hatch-linux .
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o /tmp/hatch-macos .
```

Status tests cover the complete non-promoted transition matrix, errors, preserved metadata/files/list order, changed configuration, unavailable locations, pending recovery, concurrent updates, and interruptions before/after commit. Promotion CLI tests cover every eligible starting status, retained metadata and contents, terminal guards and reserved names, exact paths and symlink aliases, changed configuration, collisions, cross-filesystem failures, interruption/recovery, ambiguous evidence, and competing mutations. A tagged filesystem-boundary hook injects EXDEV on every platform; an additional real cross-filesystem test uses `/dev/shm` when it is available on a different device (otherwise skipped).

Tests build a CLI binary, invoke separate processes with disposable homes, and assert output, exit statuses, and files—not private database layout. The `hatchtest` build tag enables internal deterministic clock and abrupt-exit controls; these controls are absent from ordinary builds. Tests cover list ordering (including same-date creations and clock changes), empty lists, missing locations, list recovery, configuration defaults, TOML validation, XDG isolation, home expansion, retained locations and recovery after configuration changes, collisions, local-date persistence, concurrent creation, and interruption before/after directory creation and during/after registry completion. Run the suite on both macOS and Linux to exercise each platform's actual filesystem behavior.
