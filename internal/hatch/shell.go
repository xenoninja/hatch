package hatch

import "fmt"

const navigationHelp = `Usage: hatch cd <name>
Enter a tracked project's directory in the current shell, including after
promotion. Requires exactly one name. Successful navigation is silent;
lookup or directory-change errors leave the working directory unchanged.

Enable shell integration by adding the appropriate line to your startup file:
  bash (~/.bashrc):             eval "$(hatch shell-init bash)"
  zsh  (~/.zshrc):              eval "$(hatch shell-init zsh)"
  fish (~/.config/fish/config.fish): hatch shell-init fish | source

Usage: hatch shell-init <bash|zsh|fish>
Print initialization code for the named shell. Hatch never edits startup files.
Use hatch path <name> to print a location without entering it.
`

func shellCommand(args []string) error {
	if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
		fmt.Print(navigationHelp)
		return nil
	}
	if args[0] == "cd" {
		if len(args) != 2 {
			return fmt.Errorf("usage: hatch cd <name>; use hatch cd --help")
		}
		if !validName.MatchString(args[1]) {
			return fmt.Errorf("invalid name %q: use lowercase ASCII letters or digits separated by single hyphens", args[1])
		}
		return fmt.Errorf("hatch cd requires shell integration; the executable cannot change its parent shell's directory\n%s", navigationHelp)
	}
	if len(args) != 2 {
		return fmt.Errorf("usage: hatch shell-init <bash|zsh|fish>")
	}
	switch args[1] {
	case "bash", "zsh":
		fmt.Print(bourneIntegration)
	case "fish":
		fmt.Print(fishIntegration)
	default:
		return fmt.Errorf("unsupported shell %q; usage: hatch shell-init <bash|zsh|fish>", args[1])
	}
	return nil
}

const bourneIntegration = `hatch() {
    if [ "${1-}" != cd ]; then
        command hatch "$@"
        return $?
    fi
    if [ "$#" -ne 2 ] || [ "$2" = --help ] || [ "$2" = -h ]; then
        command hatch "$@"
        return $?
    fi
    local hatch_destination
    # A sentinel preserves trailing newlines in a literal directory name.
    hatch_destination=$(command hatch path "$2" && printf '.') || return $?
    hatch_destination=${hatch_destination%?}
    hatch_destination=${hatch_destination%?}
    builtin cd -- "$hatch_destination"
}
`

const fishIntegration = `function hatch
    if test (count $argv) -eq 0; or test "$argv[1]" != cd
        command hatch $argv
        return $status
    end
    if test (count $argv) -ne 2; or test "$argv[2]" = --help; or test "$argv[2]" = -h
        command hatch $argv
        return $status
    end
    set -l hatch_destination (command hatch path "$argv[2]" | string collect --no-trim-newlines)
    set -l hatch_lookup_status $pipestatus[1]
    if test $hatch_lookup_status -ne 0
        return $hatch_lookup_status
    end
    # Remove only the newline added by hatch path, preserving the path itself.
    set hatch_destination (string split --right --max 1 \n -- "$hatch_destination")
    builtin cd -- "$hatch_destination[1]"
end
`
