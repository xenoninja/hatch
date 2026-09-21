# Hatch reference

Detailed command behavior, configuration, safety guarantees, recovery, and testing for [Hatch](../README.md).

## Build and use

Requires Go 1.24 or newer to build on macOS or Linux; the resulting binary needs no Go runtime or SQLite installation. Use `CGO_ENABLED=0 go build -o hatch .` for a standalone binary, then optionally install it with `install -m 0755 hatch ~/.local/bin/hatch` (create `~/.local/bin` and add it to `PATH` first). Build without the `hatchtest` tag for normal use.

```sh
go build -o hatch .
./hatch new project-alpha
./hatch info project-alpha
./hatch status project-alpha completed
./hatch status --help
./hatch promote project-alpha ~/work/project-alpha  # ~/work must already exist
./hatch promote --help
./hatch remove another-project              # interactive confirmation
./hatch remove another-project --force      # skip confirmation only
./hatch remove --help
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

## Help and version

`hatch`, `hatch help`, `hatch --help`, and `hatch -h` print identical full help with a `hatch <version>` header. `hatch --version` and `hatch version` print only `hatch <version>` followed by a newline. These commands write to stdout, exit with status 0, and do not read configuration or access project storage.

Release ZIPs embed their release tag, such as `v1.2.3`. Builds installed with `go install github.com/xenoninja/hatch@<version>` use Go's module version metadata. Unreleased source builds report `hatch dev`.

Unknown commands exit with status 1, leave stdout empty, and print a specific error and help hint to stderr:

```text
hatch: unknown command "ljldsjf"
Run 'hatch help' for available commands and examples.
```

## Path lookup

`hatch path <name>` requires exactly one project name and prints only its stored absolute location, unquoted, followed by one newline. Spaces are preserved. Lookup uses the exact name—not a prefix, case-insensitive match, or dated directory name. Promoted projects return their new location under the same name; changing `experiments_dir` does not redirect existing projects.

The location must exist and be a directory. Invalid or unknown names, incorrect argument counts, unavailable or non-directory locations, configuration/storage errors, and uncertain recovery fail with a diagnostic on stderr, nonzero exit status, and no stdout. Missing locations are not repaired and their records are not removed; use `hatch info <name>` to inspect their recorded locations.

Lookup runs the shared registry reconciliation before inspecting the project, completing verifiable interrupted operations and refusing ambiguous ones. An absent registry reports an unknown name without initializing storage. `hatch path --help` shows the shared help without accessing storage.

## Shell integration and navigation

`hatch shell-init <bash|zsh|fish>` prints sourceable code for the explicitly named
shell. Unsupported shells and missing or extra arguments fail with a diagnostic
and nonzero status. `--help` and `-h` print guidance instead of initialization code.
Generation and help do not read configuration, require a registry, or create one.

Add the appropriate line yourself:

- bash, in `~/.bashrc`: `eval "$(hatch shell-init bash)"`
- zsh, in `~/.zshrc`: `eval "$(hatch shell-init zsh)"`
- fish, in `~/.config/fish/config.fish`: `hatch shell-init fish | source`

Reload that file or start a new shell. Bash login users should source `~/.bashrc`
from `~/.bash_profile`. The Hatch executable must remain on `PATH`.
Hatch never edits shell configuration or detects a shell automatically.

The generated `hatch` function intercepts `hatch cd <name>`. It uses
`hatch path <name>` as its sole lookup and invokes the shell's built-in directory
change with one literal destination. It enters active, completed, abandoned, and
promoted projects using their stored locations, regardless of later configuration
changes. Spaces and shell metacharacters in paths are preserved. Successful
navigation is silent and returns zero. Lookup or directory-change failures report
a diagnostic, return nonzero, and leave the current directory unchanged; recovery
uncertainty is never bypassed.

Exactly one explicit name is required; missing or extra arguments show usage and
fail. `hatch cd --help` and `hatch cd -h` show guidance without lookup or navigation.
Direct executable navigation attempts with a valid name fail with setup guidance:
an executable cannot change its parent shell's directory. This guidance does not
access or initialize storage. `hatch path` continues to print a path without
changing directories. Every command other than `cd` is forwarded directly to the
executable, preserving argument boundaries, stdout, stderr, and exit status.

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

## Removal

`hatch remove <name> [--force]` ends tracking of an active, completed, or abandoned project. Existing project directories go to native trash. On **macOS**, Foundation and the system `/usr/bin/osascript` bridge choose the per-volume trash location and resolve name collisions, without Finder automation. On **Linux**, Hatch uses the [freedesktop Trash specification](https://specifications.freedesktop.org/trash-spec/latest/): payloads in `$XDG_DATA_HOME/Trash/files` and matching `.trashinfo` files in `$XDG_DATA_HOME/Trash/info` (default `~/.local/share/Trash`). Metadata contains the percent-encoded absolute original path and local deletion timestamp for desktop restoration; collision-resistant names, exclusive metadata creation, and no-replace rename prevent overwriting existing entries.

Hatch never empties trash, overwrites another trash entry, or permanently deletes as a fallback. Symlink and non-directory source entries are rejected rather than followed or deleted. Trash failures retain tracking and report an error.

### Linux requirements and limitations

Linux removal currently supports **home trash on the source filesystem only**. Per-mount `.Trash/$uid` and `.Trash-$uid` discovery is not implemented. If home trash is on another filesystem, Hatch refuses removal rather than copying and deleting or creating a nonstandard trash destination. Missing-file record-only removal still works. The trash root, `files`, and `info` must be real directories owned by the current user with mode `0700`; Hatch creates missing directories but will not repair permissions or follow symlinks at those entries. The filesystem must support exclusive rename, file/directory syncing, and the registry locking described below. An unavailable, unsafe, or unsupported trash produces an error; `--force` cannot override it.

Linux integration tests exercise real filesystem trash entries, not a desktop session. Desktop trash-browser visibility and GUI restoration require a separate check in the target desktop environment. Hatch itself provides no restore command.

If files are missing, Hatch explicitly describes **record-only removal**. Both forms prompt on a terminal and accept `y` or `yes` (case-insensitive); any other answer declines, and EOF fails without consent. Noninteractive use requires `--force`; piping `yes` is not consent. `--force` skips confirmation **only**, never lifecycle, path, trash, or recovery checks. Promoted projects are always rejected, including when their files are missing.

Successful removal releases the name for reuse. `new` still refuses any existing dated destination. Changing configuration does not redirect removal: it uses the project's recorded location. There is no restore command or permanent-deletion mode.

### Removal safety and recovery

Removal holds the same registry lock through confirmation, trash, and registry commit, so competing removals cannot both act on a project. Before the native trash call, a durable intent records the name, source, and directory device/inode, then marks the native operation as potentially in flight. This marker prevents recovery from clearing evidence while an orphaned macOS helper could still move files after Hatch is killed. A synchronously returned failure clears that marker before checking whether the unchanged source proves a safe failure. On macOS, Hatch persists the returned exact trash path after Foundation succeeds.

On Linux, Hatch persists the selected payload path and exact expected `.trashinfo` contents **before creating either entry**. It exclusively creates, writes, and syncs metadata and its parent before moving the payload with an atomic no-replace rename. Recovery requires the exact regular metadata file as well as the payload identity; a missing, partial, changed, or symlink metadata entry cannot release tracking. A crash after the payload move can therefore recover even before the native call returns. A planned operation interrupted before the payload move conservatively blocks for manual investigation, reporting both trash paths—even if the source is still present. Hatch leaves partial metadata as evidence and never guesses whether an existing entry belongs to another trash client.

After a successful move, Hatch verifies the moved directory identity before deleting the project record and intent in one transaction. On Linux, Hatch first syncs both move parents and restoration metadata. On macOS, Hatch syncs the source parent and relies on Foundation for the destination-side trash operation: macOS may deny opening the trash parent even when the native move and receipt identity check succeed. Hatch does not independently sync the macOS trash parent. Record-only removal needs just that atomic registry transaction and rechecks absence after consent. Existing registries are upgraded under the shared lock without changing pending macOS recovery semantics.

On the next registry command:

- No receipt, no potentially in-flight helper, and the original identified source remains: clear the unexecuted intent and retain tracking.
- A receipt exists, the source is absent, and the trash directory matches the recorded identity (with exact restoration metadata for Linux): perform the platform-specific syncing described above and finish ending tracking.
- A potentially in-flight operation without a receipt (even if the source still exists), source missing without a receipt (including a crash after macOS moved files but before Hatch recorded the result), an incomplete Linux plan, changed identities, both paths present/missing, or inaccessible/invalid evidence: retain tracking and pending evidence, report the source and any payload/metadata paths, and block registry commands, including forced removal. Help stays available.

Recovery never searches or modifies other trash entries. An uncertain outcome requires manual investigation; preserve the registry and reported paths rather than deleting pending evidence. In particular, moving or emptying the trash entry before recovery may make an interrupted removal ambiguous. The advisory lock coordinates Hatch processes sharing one registry, not unrelated programs editing files concurrently.

## Configuration and storage

Hatch reads `$XDG_CONFIG_HOME/hatch/config.toml`, defaulting to `~/.config/hatch/config.toml`. Create this file manually to override the experiments directory:

```toml
experiments_dir = "~/work/experiments"
```

`experiments_dir` accepts an absolute path or a path starting with `~/` (expanded using your home directory). Existing symlinks are resolved before subsequent `..` components, so configured paths retain their filesystem meaning. Other relative paths, empty values, non-string values, malformed TOML, and paths whose existing components are not usable directories produce errors rather than falling back. Missing configuration or an omitted key uses `~/experiments`. Missing experiments directories are allowed and created only when needed by `new`.

The registry and its lock live under `$XDG_DATA_HOME/hatch/`, defaulting to `~/.local/share/hatch/`. XDG homes must be absolute; unset, empty, or relative values use their respective defaults. XDG values are not shell-expanded by Hatch. Different data homes have independent registries; changing the data home does not copy or discover old records.

Changing `experiments_dir` affects only subsequently created projects. Existing projects and interrupted creations retain their stored absolute locations; no files are moved. Tracked locations, including missing locations, remain reserved: creation and promotion reject paths equal to, inside, or containing another tracked project, including symlink aliases. Promotion and removal also reject existing overlapping tracked locations before moving files, so an operation cannot indirectly move or trash another project. Inspection still reconciles pending operations in an existing registry, even after configuration changes. Invalid configuration fails before accessing storage; help remains available.

Hatch never creates the configuration file or configuration directories. Help and inspection against an absent registry do not create data or experiments directories.

## Creation safety and recovery

Commands accessing an existing registry share an OS advisory lock (`mutation.lock` beside the registry). SQLite uses fully synchronous transactions and a unique name constraint. A durable pending-creation record precedes directory creation. After syncing the directory and its parent, Hatch records the directory's device/inode identity. Registry insertion and clearing the pending record commit in one transaction.

On a subsequent invocation:

- Intent with no destination is cleared; creation can be retried.
- A durably identified, unchanged directory completes registration with the original date.
- A committed project remains tracked, even if its files later disappear.
- An existing destination without recorded ownership, a changed directory identity, or a missing previously identified directory is uncertain. Hatch preserves files and pending evidence, reports the path, and blocks operations requiring recovery. This includes `info`, `path`, `list`, and `status`; help remains available.

In particular, interruption between making the directory and recording its identity deliberately requires manual investigation—even if the directory is empty. Hatch never guesses ownership or deletes files during recovery. There is no automatic repair command in this release. Preserve the reported path and the registry before investigating; deleting the registry or pending evidence is not a safe generic repair.

Coordination assumes a local filesystem supporting SQLite locking, advisory locks, and directory syncing. It coordinates Hatch processes, not unrelated programs editing paths concurrently. Device/inode identity is evidence for ordinary restart recovery, not protection against malicious filesystem manipulation or inode reuse.

## Promotion safety and recovery

Promotion holds the same registry lock as creation, status changes, inspection, and recovery. Before moving anything it durably records the project name, source, exact destination, and source device/inode identity. After the rename, both parent directories are synced before the status/location update and intent deletion commit together.

On the next registry command, even with changed configuration:

- The original identified source exists and the destination is absent: clear the unexecuted intent, retaining the original status/location. Promotion can be retried.
- The source is absent and the destination has the recorded directory identity: sync the parent directories and complete promotion.
- Both paths exist, both are missing, either identity changed, or a path cannot be inspected reliably: preserve files and pending evidence, report both paths, and block registry commands (`new`, `info`, `path`, `list`, `status`, and `promote`). Help remains available.

As with creation recovery, ambiguous states require manual investigation; do not delete registry evidence as a generic repair. The shared lock coordinates Hatch processes using the same registry, not unrelated filesystem edits or independent data homes. Exclusive rename also prevents a destination that appears at move time from being overwritten.

## Tests

Install bash, zsh, and fish before running the suite; shell integration tests require
all three and fail if one is unavailable. Tests run generated integration in real
shell processes and observe directory changes, failure behavior, and forwarding.

```sh
go vet ./...
go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/hatch-linux .
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o /tmp/hatch-macos .
```

Removal CLI tests cover confirmation/decline/EOF, noninteractive refusal, force, missing files, promoted-to-remove guards, eligible statuses, trash failures/collisions, name reuse and dated destination refusal, competing removals, interruption points, and ambiguous recovery. Real macOS and Linux integration tests trash disposable projects and verify contents at the returned paths. macOS cleanup is limited to the identity-checked entry; Linux trash and metadata live entirely inside disposable test homes. Linux-specific tests cover path encoding and deletion dates, name reuse, unsafe/symlink trash, a real second-filesystem refusal, payload/metadata collisions with another trash client, interrupted metadata/payload/registry operations, damaged metadata, and competing native remove/promote operations. Tagged terminal and trash-boundary controls exercise failure/recovery deterministically without affecting production builds. CI runs the shared suite, vet, and standalone builds on both macOS and Linux.

Status tests cover the complete non-promoted transition matrix, errors, preserved metadata/files/list order, changed configuration, unavailable locations, pending recovery, concurrent updates, and interruptions before/after commit. Promotion CLI tests cover every eligible starting status, retained metadata and contents, terminal guards and reserved names, exact paths and symlink aliases, changed configuration, collisions, cross-filesystem failures, interruption/recovery, ambiguous evidence, and competing mutations. A tagged filesystem-boundary hook injects EXDEV on every platform; an additional real cross-filesystem test uses `/dev/shm` when it is available on a different device (otherwise skipped).

Tests build a CLI binary, invoke separate processes with disposable homes, and assert output, exit statuses, and files—not private database layout. The `hatchtest` build tag enables internal deterministic clock and abrupt-exit controls; these controls are absent from ordinary builds. Tests cover list ordering (including same-date creations and clock changes), empty lists, missing locations, list recovery, configuration defaults, TOML validation, XDG isolation, home expansion, retained locations and recovery after configuration changes, collisions, local-date persistence, concurrent creation, and interruption before/after directory creation and during/after registry completion. Run the suite on both macOS and Linux to exercise each platform's actual filesystem behavior.
