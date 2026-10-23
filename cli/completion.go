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

const bashCompletion = `# nflow bash completion.
# Install:
#   nflow completion bash | sudo tee /etc/bash_completion.d/nflow > /dev/null
# Or per user:
#   nflow completion bash > ~/.local/share/bash-completion/completions/nflow
_nflow() {
    local cur prev subcommands flags sub op
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    subcommands="init run build serve lint explain expand catalog list show completion self require pin info help version"

    if [ "$COMP_CWORD" -eq 1 ]; then
        COMPREPLY=($(compgen -W "$subcommands" -- "$cur"))
        return
    fi

    sub="${COMP_WORDS[1]}"
    op="${COMP_WORDS[2]}"
    case "$sub" in
        run) flags="-v -vv -vvv --assert= --approval= --budget= --max-tokens= --journal-dir= --execution-id=" ;;
        build) flags="-o --output= --debug -d --no-trimpath --all-bundles" ;;
        completion) flags="bash zsh pwsh powershell" ;;
        self)
            if [ "$COMP_CWORD" -eq 2 ]; then flags="up test bench"
            elif [[ "$op" == "bench" ]]; then flags="--path --iterations --warmup --json --list"
            elif [[ "$op" == "test" ]]; then flags="--verbose --json --no-color"
            else flags=""
            fi ;;
        require)
            if [ "$COMP_CWORD" -eq 2 ]; then flags="check update pin reset"
            elif [[ "$op" == "check" || "$op" == "update" || "$op" == "pin" ]]; then flags="--package"
            else flags=""
            fi ;;
        pin) flags="--package" ;;
        *) flags="" ;;
    esac

    if [[ "$sub" == "self" && "$op" == "bench" && ( "$prev" == "--path" || "$prev" == "--iterations" || "$prev" == "--warmup" ) ]]; then
        COMPREPLY=()
        return
    fi

    if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "$flags" -- "$cur"))
        return
    fi

    case "$sub" in
        completion) COMPREPLY=($(compgen -W "$flags" -- "$cur")) ;;
        self)
            if [ "$COMP_CWORD" -eq 2 ]; then
                COMPREPLY=($(compgen -W "up test bench" -- "$cur"))
            else
                COMPREPLY=()
            fi ;;
        require)
            if [ "$COMP_CWORD" -eq 2 ]; then
                COMPREPLY=($(compgen -W "check update pin reset" -- "$cur"))
            elif [[ "$op" == "check" || "$op" == "update" || "$op" == "pin" || "$op" == "reset" ]]; then
                if [[ "$prev" == "--package" ]]; then return; fi
                COMPREPLY=($(compgen -f -X '!*.nflow' -- "$cur"))
            fi ;;
        pin)
            if [[ "$prev" == "--package" ]]; then COMPREPLY=(); else COMPREPLY=($(compgen -f -X '!*.nflow' -- "$cur")); fi ;;
        run|build|serve|lint|explain|expand|list|info)
            COMPREPLY=($(compgen -f -X '!*.nflow' -- "$cur")) ;;
    esac
}
complete -F _nflow nflow
`

const zshCompletion = `#compdef nflow
# nflow zsh completion.
# Install: nflow completion zsh > "${fpath[1]}/_nflow"
_nflow() {
    local -a subcommands
    subcommands=(
        'run:run a .nflow pipeline'
        'build:compile a .nflow into a standalone executable'
        'serve:validate a flow for serve mode'
        'lint:check .nflow sources'
        'explain:inspect a .nflow file'
        'expand:show macro expansions'
        'catalog:dump the compiler catalog'
        'list:list compiler capabilities'
        'show:show one compiler capability'
        'init:initialize a project or bundle'
        'self:manage the nflow binary, run self-tests, or benchmark scenarios'
        'require:generate glue or manage direct @require selectors'
        'pin:alias for require pin'
        'info:describe a .nflow file'
        'completion:print a shell completion script'
        'version:print build info'
        'help:print usage'
    )

    if (( CURRENT == 2 )); then
        _describe -t commands 'nflow command' subcommands
        return
    fi

    local sub="${words[2]}"
    case "$sub" in
        run)
            _arguments '1:flow file:_files -g "*.nflow"' \
                '-v' '-vv' '-vvv' '--assert=' '--approval=' '--budget=' '--max-tokens=' \
                '--journal-dir=' '--execution-id='
            ;;
        build)
            _arguments '1:flow file:_files -g "*.nflow"' \
                '-o:output file:_files' '--output=:output file:_files' \
                '--debug[keep debug symbols]' '-d[keep debug symbols]' \
                '--no-trimpath[keep full source paths]' '--all-bundles[link all native extensions]'
            ;;
        info|serve|lint|explain|expand)
            _arguments '1:flow file:_files -g "*.nflow"'
            ;;
        self)
            if (( CURRENT == 3 )); then
                _values 'operation' up test bench
            elif [[ "${words[3]}" == "bench" ]]; then
                _arguments \
                    '--path=[use a custom file or directory]:file or directory:()' \
                    '--iterations=[number of measured iterations]:N:()' \
                    '--warmup=[number of warmup iterations]:N:()' \
                    '--json[emit a JSON report]' \
                    '--list[list matching scenarios without running]'
            fi
            ;;
        completion)
            _values 'shell' bash zsh pwsh powershell
            ;;
        require)
            if (( CURRENT == 3 )); then
                _values 'operation' check update pin reset
            elif [[ "${words[3]}" == "reset" ]]; then
                _arguments '1:operation:(reset)' '2:flow file:_files -g "*.nflow"'
            else
                _arguments '1:operation:(check update pin reset)' '2:flow file:_files -g "*.nflow"' \
                    '--package=[select requirements by exact import or glob]:pattern:'
            fi
            ;;
        pin)
            _arguments '1:flow file:_files -g "*.nflow"' \
                '--package=[select requirements by exact import or glob]:pattern:'
            ;;
    esac
}
_nflow "$@"
`

//
//nolint:gosec // G101 false positive: shell completion script, not credentials.
const pwshCompletion = `# nflow PowerShell completion.
# Install: nflow completion pwsh | Out-String | Invoke-Expression
# Or append to your $PROFILE:
#   nflow completion pwsh >> $PROFILE
Register-ArgumentCompleter -Native -CommandName nflow -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)

    $subcommands = @('init', 'run', 'build', 'serve', 'lint', 'explain', 'expand', 'catalog', 'list', 'show', 'completion', 'self', 'require', 'pin', 'info', 'help', 'version')
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

    if ($sub -eq 'self' -and ($tokens.Count -le 2 -or
        ($tokens.Count -eq 3 -and $tokens[2].ToString() -notin @('up', 'test', 'bench')))) {
        emit @('up', 'test', 'bench') 'ParameterValue'
        return
    }

    if ($sub -eq 'self' -and $tokens.Count -ge 4 -and
        $tokens[-2].ToString() -in @('--path', '--iterations', '--warmup')) {
        return
    }

	if ($sub -eq 'require' -and ($tokens.Count -le 2 -or ($tokens.Count -eq 3 -and $tokens[2].ToString() -notin @('check', 'update', 'pin', 'reset')))) {
        emit @('check', 'update', 'pin', 'reset') 'ParameterValue'
        return
    }

    $flags = switch ($sub) {
        'run'   { @('-v', '-vv', '-vvv', '--assert=', '--approval=', '--budget=', '--max-tokens=', '--journal-dir=', '--execution-id=') }
        'build' { @('-o', '--output=', '--debug', '-d', '--no-trimpath', '--all-bundles') }
		'self' { if ($tokens.Count -ge 3 -and $tokens[2].ToString() -eq 'bench') { @('--path', '--iterations', '--warmup', '--json', '--list') } elseif ($tokens.Count -ge 3 -and $tokens[2].ToString() -eq 'test') { @('--verbose', '--json', '--no-color') } else { @() } }
		'require' { if ($tokens.Count -ge 3 -and $tokens[2].ToString() -in @('check', 'update', 'pin')) { @('--package') } else { @() } }
        'pin' { @('--package') }
        default { @() }
    }

    if ($wordToComplete -like '-*') {
        emit $flags 'ParameterName'
        return
    }

    if ($sub -in @('run', 'build', 'serve', 'lint', 'explain', 'expand', 'info', 'pin') -or
		($sub -eq 'require' -and $tokens.Count -ge 3 -and $tokens[2].ToString() -in @('check', 'update', 'pin', 'reset'))) {
        $pattern = if ($wordToComplete) { "$wordToComplete*.nflow" } else { "*.nflow" }
        Get-ChildItem -Filter $pattern -File -ErrorAction SilentlyContinue | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new(
                $_.Name, $_.Name, 'ProviderItem', $_.FullName)
        }
    }
}
`
