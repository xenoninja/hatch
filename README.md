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

Use `hatch --help` or `hatch <command> --help` for more options.

Projects start as **active**. You can switch freely among active, completed, and abandoned without touching their files. Promotion sets the terminal status **promoted**: the project stays listed, but Hatch no longer allows status changes, promotion, or removal for it.

## Shell navigation

Add the line for your shell to its startup file, then reload the file or open a new terminal:

| Shell | Startup file | Line to add |
| --- | --- | --- |
| bash | `~/.bashrc` | `eval "$(hatch shell-init bash)"` |
| zsh | `~/.zshrc` | `eval "$(hatch shell-init zsh)"` |
| fish | `~/.config/fish/config.fish` | `hatch shell-init fish \| source` |


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
