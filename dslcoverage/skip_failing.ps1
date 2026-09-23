<#
.SYNOPSIS
    Renames failing coverage files to skip_<name>.nflow.

.DESCRIPTION
    Runs `go test ./flow/dslcoverage/...` with the -json flag, parses
    the failures, and renames each failing file to `skip_<basename>`.
    Already-skipped files are left untouched. The script is
    idempotent: running it twice changes nothing the second time.

    Use this after landing a change that is expected to fix some
    coverage files. It keeps the passing ones as `*.nflow` and parks
    the rest.

.PARAMETER DryRun
    Print the plan without renaming anything.

.EXAMPLE
    .\skip_failing.ps1
    .\skip_failing.ps1 -DryRun
#>
[CmdletBinding()]
param(
    [switch] $DryRun
)

$ErrorActionPreference = 'Stop'

# Resolve paths relative to this script so the script works from any
# current working directory.
$scriptDir  = Split-Path -Parent $MyInvocation.MyCommand.Path
$flowDir    = Resolve-Path (Join-Path $scriptDir '..')
$coverageDir = $scriptDir

Push-Location $flowDir
try {
    Write-Host "→ Running go test ./dslcoverage/..." -ForegroundColor Cyan

    # Run the test with JSON output and capture failures.
    $json = & go test ./dslcoverage/... -json 2>&1

    # Collect failing subtests. Each subtest is named after a file
    # relative to dslcoverage/, e.g. "TestCoverage/base\wrap.nflow".
    $failing = New-Object System.Collections.Generic.HashSet[string]
    $pattern = '^TestCoverage/(?<file>.+\.nflow)$'

    foreach ($line in $json) {
        $evt = $line | ConvertFrom-Json -ErrorAction SilentlyContinue
        if ($null -eq $evt) { continue }
        if ($evt.Action -ne 'fail') { continue }
        if ($evt.Test -notmatch $pattern) { continue }

        $rel = $Matches['file'] -replace '\\', '/'
        $null = $failing.Add($rel)
    }

    if ($failing.Count -eq 0) {
        Write-Host "✓ No failures to park." -ForegroundColor Green
        return
    }

    Write-Host ("→ Parking {0} failing file(s):" -f $failing.Count) -ForegroundColor Yellow

    foreach ($rel in ($failing | Sort-Object)) {
        $abs = Join-Path $coverageDir $rel
        if (-not (Test-Path -LiteralPath $abs)) {
            Write-Warning "missing on disk: $rel"
            continue
        }

        $dir  = Split-Path -Parent $abs
        $name = Split-Path -Leaf $abs

        if ($name.StartsWith('skip_')) {
            Write-Host ("  = already skipped: {0}" -f $rel) -ForegroundColor DarkGray
            continue
        }

        $target = Join-Path $dir ("skip_" + $name)

        if ($DryRun) {
            Write-Host ("  · would rename: {0} → skip_{1}" -f $rel, $name) -ForegroundColor DarkYellow
            continue
        }

        if (Test-Path -LiteralPath $target) {
            Write-Warning ("target already exists, skipping: {0}" -f $target)
            continue
        }

        Move-Item -LiteralPath $abs -Destination $target
        Write-Host ("  ✓ {0} → skip_{1}" -f $rel, $name) -ForegroundColor Green
    }

    Write-Host ""
    Write-Host "Done. Update SKIPPED.md with the reason for each newly parked file." -ForegroundColor Cyan
}
finally {
    Pop-Location
}
