# Hatch

**A home for your next experiment.**

Hatch is a small CLI for managing experimental projects on macOS and Linux. Start an idea, keep track of its progress, and decide what happens next: keep it, move it into real work, or send it to the trash.

No templates or scaffolding—just an empty directory and a record of your project.

## Why Hatch?

- **Start quickly.** Create dated project folders with a single command.
- **Keep experiments organized.** List projects and mark them active, completed, or abandoned.
- **Graduate the good ones.** Promote an experiment by moving it to a new home without changing its contents.
- **Clean up safely.** Send old experiments to native trash, never permanent deletion.
- **Stay lightweight.** One standalone binary; no Go runtime or SQLite installation needed to run it.

## Install

### From GitHub Releases

Download the ZIP for your operating system and architecture from [GitHub Releases](https://github.com/xenoninja/hatch/releases): `darwin` for macOS or `linux` for Linux, and `arm64` or `amd64` for your CPU. No Go installation is needed.

Extract the archive, then run these commands from the extracted directory:

```sh
mkdir -p ~/.local/bin
install -m 0755 hatch ~/.local/bin/hatch
```

Make sure `~/.local/bin` is on your `PATH`.

### With go install

With [Go 1.24 or newer](https://go.dev/dl/):

```sh
go install github.com/xenoninja/hatch@latest
```

Make sure `$(go env GOPATH)/bin` is on your `PATH` (or `GOBIN` if you've set it).

### Build from source

With [Go 1.24 or newer](https://go.dev/dl/):

```sh
git clone https://github.com/xenoninja/hatch.git
cd hatch
CGO_ENABLED=0 go build -o hatch .
mkdir -p ~/.local/bin
install -m 0755 hatch ~/.local/bin/hatch
```

Make sure `~/.local/bin` is on your `PATH`.

## Quick start

```sh
# Give an idea a home
hatch new tiny-search
# Creates ~/experiments/YYYY-MM-DD-tiny-search

# See what you're working on
hatch list
hatch info tiny-search

# Enter the folder and do stuff
hatch                    # Search and choose a project (requires shell setup)
hatch cd tiny-search

# Finish the experiment, keeping its files
hatch status tiny-search completed

# Ready for something bigger? Move it out of experiments
hatch promote tiny-search ~/work/tiny-search
```

For an idea you'd rather set aside:

```sh
hatch new another-idea
hatch status another-idea abandoned
hatch remove another-idea  # Asks for confirmation, then moves it to trash
```

Names use lowercase letters or digits separated by single hyphens, such as `tiny-search` or `test-2`.

## Commands

| Command | What it does |
| --- | --- |
| `hatch` | Search tracked project names and enter one in your current shell (requires setup below). |
| `hatch new <name>` | Create an empty, dated experiment directory. |
| `hatch list` | List all tracked projects, newest first. |
| `hatch info <name>` | Show a project's creation date, status, and location. |
| `hatch path <name>` | Print only the current absolute directory path, including after promotion. |
| `hatch cd <name>` | Enter the project in your current shell (requires setup below). |
| `hatch shell-init <bash\|zsh\|fish>` | Print optional shell initialization code. |
| `hatch status <name> <status>` | Mark a project `active`, `completed`, or `abandoned`. |
| `hatch promote <name> <target-path>` | Move a project to an exact destination outside experiments. |
| `hatch remove <name>` | Move a project to trash and stop tracking it. |

`hatch path <name>` prints the unquoted path followed by a newline. Invalid or unknown names, missing or non-directory locations, and uncertain recovery fail with a nonzero exit status, a diagnostic on stderr, and no stdout. See the [path lookup reference](docs/reference.md#path-lookup).

Run `hatch help`, `hatch --help`, or `hatch -h` for the same full help, including the installed version. Use `hatch <command> --help` for command guidance. `hatch --version` (or `hatch version`) prints only the version.

Projects start as **active**. You can switch freely among active, completed, and abandoned without touching their files. Promotion sets the terminal status **promoted**: the project stays listed, but Hatch no longer allows status changes, promotion, or removal for it.

## Shell navigation

Add the line for your shell to its startup file, then reload the file or open a new terminal:

| Shell | Startup file | Line to add |
| --- | --- | --- |
| bash | `~/.bashrc` | `eval "$(hatch shell-init bash)"` |
| zsh | `~/.zshrc` | `eval "$(hatch shell-init zsh)"` |
| fish | `~/.config/fish/config.fish` | `hatch shell-init fish \| source` |

With integration loaded, plain `hatch` opens a searchable browser of all
tracked projects, including promoted ones. Type to filter names: matching is
literal, ignores case, and allows gaps (`MWA` matches `my-web-app`). Matched
characters are highlighted. An empty query lists projects alphabetically;
searches rank exact names, prefixes, contiguous substrings, then gapped matches.
Gapped matches prefer fewer skipped characters; remaining ties prefer shorter
names, then alphabetical order. Status and location are not searched.

Backspace edits the query and Ctrl-U clears it. Each query change selects the
first result. Up/Down or Ctrl-P/Ctrl-N move the highlight, Enter enters the
selected directory, and Esc or Ctrl-C cancels. Enter is required even for one
match; with no matches it does nothing so you can edit or cancel. The browser
shows a search field, match count, names and statuses, with up to ten rows that
scroll. Missing or non-directory locations stay searchable as unavailable;
selecting one explains the problem and keeps the browser open.

Results stay stable while browsing. Ctrl-R refreshes projects, statuses, and
availability while preserving your query and selecting the first match. Other
Hatch commands can run while the browser waits for input. Enter rechecks the
selected project's identity and location: removal, same-name recreation, or
promotion refreshes results with an explanation and requires another Enter before
navigation. A status-only change permits navigation. If refresh finds no query
matches, keep editing or cancel; an empty registry exits successfully with
`No experimental projects tracked.` and leaves your directory unchanged.

After upgrading, reload your startup file or start a new shell to install the
updated function. Direct executable invocation shows setup guidance.
Noninteractive bare invocation fails promptly; use `hatch help` or `hatch list`
in scripts. `hatch cd <name>` continues to enter a project by its exact name.


## Configuration

By default, experiments live in `~/experiments`. To choose another directory, create `~/.config/hatch/config.toml`:

```toml
experiments_dir = "~/work/experiments"
```

Use an absolute path or one starting with `~/`. Changing this setting affects only new projects; existing projects keep their recorded locations.

Hatch stores its registry at `~/.local/share/hatch/hatch.db` and respects `XDG_CONFIG_HOME` and `XDG_DATA_HOME`. It tracks only projects created through Hatch—it doesn't scan or import existing folders.

## Your files come first

- Existing destinations are never overwritten or merged.
- Promotion requires an existing parent directory and a destination on the **same filesystem** as the experiment.
- Removal uses native trash. On Linux, only home trash on the **same filesystem** is supported; unsupported or unsafe trash locations cause an error, not permanent deletion.
- `hatch remove --force` skips confirmation only—not safety checks. If files are already missing, removal only clears the tracking record.
- Interrupted operations are recovered when their outcome can be verified. If the outcome is uncertain, Hatch preserves the evidence and stops for manual investigation. Don't delete the registry to bypass a recovery error.

Hatch has no restore command. See the [reference](docs/reference.md) for platform requirements and detailed recovery behavior.

## Development

```sh
go vet ./...
go test ./...
```

Tests exercise the CLI with disposable homes and projects. See the [testing reference](docs/reference.md#tests) for platform coverage and standalone build checks.

Found a bug or have an idea? [Open an issue](https://github.com/xenoninja/hatch/issues).
