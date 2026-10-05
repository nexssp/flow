package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// SupportedShells lists the shells for which completion scripts exist.
var SupportedShells = []string{"bash", "zsh", "pwsh", "powershell"}

// Completion writes a completion script for the named shell to w.
func Completion(shell string, w io.Writer) error {
	shell = strings.ToLower(strings.TrimSpace(shell))
	switch shell {
	case "bash":
		_, err := io.WriteString(w, bashCompletion)
		return err
	case "zsh":
		_, err := io.WriteString(w, zshCompletion)
		return err
	case "pwsh", "powershell":
		_, err := io.WriteString(w, pwshCompletion)
		return err
	case "":
		return fmt.Errorf("completion: shell name required (one of: %s)", strings.Join(SupportedShells, ", "))
	default:
		return fmt.Errorf("completion: unsupported shell %q (supported: %s)", shell, strings.Join(SupportedShells, ", "))
	}
}

// ErrCompletionEmpty is reserved for future dynamic completions that
// may return nothing. Kept for API stability.
var ErrCompletionEmpty = errors.New("completion: nothing to complete")

// ── bash ─────────────────────────────────────────────────────────────

const bashCompletion = `# nexssflow bash completion.
# Install:
#   nexssflow completion bash | sudo tee /etc/bash_completion.d/nexssflow > /dev/null
# Or per user:
#   nexssflow completion bash > ~/.local/share/bash-completion/completions/nexssflow
_nexssflow() {
    local cur prev subcommands flags sub
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    subcommands="run build info test help version selfbuild completion"

    if [ "$COMP_CWORD" -eq 1 ]; then
        COMPREPLY=($(compgen -W "$subcommands" -- "$cur"))
        return
    fi

    sub="${COMP_WORDS[1]}"

    case "$sub" in
        run)
            flags="-v -vv -vvv --assert= --approval= --budget= --max-tokens="
            ;;
	        build)
	            flags="-o --output="
            ;;
        completion)
            flags="bash zsh pwsh powershell"
            ;;
        *)
            flags=""
            ;;
    esac

    if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "$flags" -- "$cur"))
        return
    fi

    case "$sub" in
        completion)
            COMPREPLY=($(compgen -W "$flags" -- "$cur"))
            ;;
        run|info|test|build)
            COMPREPLY=($(compgen -f -X '!*.nflow' -- "$cur"))
            ;;
    esac
}
complete -F _nexssflow nexssflow
`

// ── zsh ──────────────────────────────────────────────────────────────

const zshCompletion = `#compdef nexssflow
# nexssflow zsh completion.
# Install: nexssflow completion zsh > "${fpath[1]}/_nexssflow"
_nexssflow() {
    local -a subcommands
    subcommands=(
        'run:run a .nflow pipeline'
        'build:compile a .nflow into a standalone executable'
        'info:describe a .nflow file'
        'test:run a .nflow in test mode'
        'selfbuild:rebuild the nexssflow binary with VCS metadata'
        'completion:print a shell completion script'
        'version:print build info'
        'help:print usage'
    )

    if (( CURRENT == 2 )); then
        _describe -t commands 'nexssflow command' subcommands
        return
    fi

    local sub="${words[2]}"
    case "$sub" in
        run)
            _arguments '1:flow file:_files -g "*.nflow"' \
                '-v' '-vv' '-vvv' \
                '--assert=' '--approval=' '--budget=' '--max-tokens='
            ;;
	        build)
	            _arguments '1:flow file:_files -g "*.nflow"' \
	                '-o:output file:_files' '--output=:output file:_files'
            ;;
        info|test)
            _arguments '1:flow file:_files -g "*.nflow"'
            ;;
        completion)
            _values 'shell' bash zsh pwsh powershell
            ;;
    esac
}
_nexssflow "$@"
`

// ── pwsh ─────────────────────────────────────────────────────────────

const pwshCompletion = `# nexssflow PowerShell completion.
# Install: nexssflow completion pwsh | Out-String | Invoke-Expression
# Or append to your $PROFILE:
#   nexssflow completion pwsh >> $PROFILE
Register-ArgumentCompleter -Native -CommandName nexssflow -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)

    $subcommands = @('run', 'build', 'info', 'test', 'selfbuild', 'completion', 'version', 'help')
    $shells      = @('bash', 'zsh', 'pwsh', 'powershell')

    function emit([string[]]$values, [string]$kind) {
        foreach ($v in $values) {
            if ($v -like "$wordToComplete*") {
                [System.Management.Automation.CompletionResult]::new($v, $v, $kind, $v)
            }
        }
    }

    $tokens = $commandAst.CommandElements

    if ($tokens.Count -le 1 -or
        ($tokens.Count -eq 2 -and $cursorPosition -le $tokens[1].Extent.EndOffset)) {
        emit $subcommands 'ParameterValue'
        return
    }

    $sub = $tokens[1].ToString()

    if ($sub -eq 'completion') {
        emit $shells 'ParameterValue'
        return
    }

    $flags = switch ($sub) {
        'run'   { @('-v', '-vv', '-vvv', '--assert=', '--approval=', '--budget=', '--max-tokens=') }
		'build' { @('-o', '--output=') }
        default { @() }
    }

    if ($wordToComplete -like '-*') {
        emit $flags 'ParameterName'
        return
    }

    if ($sub -in @('run', 'build', 'info', 'test')) {
        $pattern = if ($wordToComplete) { "$wordToComplete*.nflow" } else { "*.nflow" }
        Get-ChildItem -Filter $pattern -File -ErrorAction SilentlyContinue | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new(
                $_.Name, $_.Name, 'ProviderItem', $_.FullName)
        }
    }
}
`
