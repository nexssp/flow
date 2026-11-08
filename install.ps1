[CmdletBinding()]
param(
    [string]$Version = 'latest',
    [string]$InstallDir = '',
    [switch]$NoPathUpdate
)

$ErrorActionPreference = 'Stop'
$repo = 'nexssp/flow'
$headers = @{
    Accept = 'application/vnd.github+json'
}

if ([string]::IsNullOrWhiteSpace($InstallDir)) {
    if (-not [string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
        $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\nflow'
    }
    else {
        $InstallDir = Join-Path $HOME '.local\bin'
    }
}

if ($Version -eq 'latest') {
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest" -Headers $headers -UserAgent 'nflow-installer'
        $Version = [string]$release.tag_name
    }
    catch {
        throw "Could not resolve the latest stable GitHub release: $($_.Exception.Message)"
    }
}

$Version = $Version -replace '^[vV]', ''
if ($Version -notmatch '^[0-9A-Za-z][0-9A-Za-z._+-]*$') {
    throw "Invalid release version: $Version"
}

$processor = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
$arch = switch -Regex ($processor) {
    '^(AMD64|x64)$' { 'amd64'; break }
    '^(ARM64|aarch64)$' { 'arm64'; break }
    default { throw "Unsupported Windows architecture: $processor" }
}

$archiveName = "nflow_${Version}_windows_${arch}.zip"
$checksumsName = "nflow_${Version}_checksums.txt"
$releaseUrl = "https://github.com/$repo/releases/download/v$Version"
$workDir = Join-Path ([IO.Path]::GetTempPath()) ("nflow-install-" + [Guid]::NewGuid().ToString('N'))
$stagePath = $null

try {
    [void][IO.Directory]::CreateDirectory($workDir)
    $checksumsPath = Join-Path $workDir $checksumsName
    $archivePath = Join-Path $workDir $archiveName
    $extractDir = Join-Path $workDir 'extracted'

    try {
        Invoke-WebRequest -Uri "$releaseUrl/$checksumsName" -OutFile $checksumsPath -Headers $headers -UserAgent 'nflow-installer' -UseBasicParsing
        Invoke-WebRequest -Uri "$releaseUrl/$archiveName" -OutFile $archivePath -Headers $headers -UserAgent 'nflow-installer' -UseBasicParsing
    }
    catch {
        throw "Could not download the $Version release assets: $($_.Exception.Message)"
    }

    $expectedHash = $null
    foreach ($line in [IO.File]::ReadLines($checksumsPath)) {
        if ($line -match '^\s*([A-Fa-f0-9]{64})\s+\*?(.+?)\s*$' -and $Matches[2] -ceq $archiveName) {
            if ($null -ne $expectedHash) {
                throw "Multiple checksum entries found for $archiveName."
            }
            $expectedHash = $Matches[1].ToLowerInvariant()
        }
    }
    if ($null -eq $expectedHash) {
        throw "No checksum entry found for $archiveName."
    }

    $actualHash = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualHash -cne $expectedHash) {
        throw "SHA-256 verification failed for $archiveName."
    }

    Expand-Archive -LiteralPath $archivePath -DestinationPath $extractDir
    $binaryPath = Join-Path $extractDir 'nflow.exe'
    if (-not (Test-Path -LiteralPath $binaryPath -PathType Leaf)) {
        throw 'The release archive does not contain nflow.exe.'
    }

    [void][IO.Directory]::CreateDirectory($InstallDir)
    $InstallDir = [IO.Path]::GetFullPath($InstallDir)
    $destination = Join-Path $InstallDir 'nflow.exe'
    $stagePath = Join-Path $InstallDir ('.nflow.exe.' + [Guid]::NewGuid().ToString('N') + '.tmp')
    Copy-Item -LiteralPath $binaryPath -Destination $stagePath
    Move-Item -LiteralPath $stagePath -Destination $destination -Force
    $stagePath = $null

    Write-Host "Installed nflow $Version at $destination"
    if ($NoPathUpdate) {
        Write-Host 'PATH was not changed. Add the install directory to PATH if needed.'
    }
    else {
        $normalizedInstallDir = $InstallDir.TrimEnd([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar)
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $pathExists = $false
        foreach ($entry in ($userPath -split ';')) {
            if (-not [string]::IsNullOrWhiteSpace($entry)) {
                $normalizedEntry = $entry.Trim().TrimEnd([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar)
                if ([string]::Equals($normalizedEntry, $normalizedInstallDir, [StringComparison]::OrdinalIgnoreCase)) {
                    $pathExists = $true
                    break
                }
            }
        }

        if (-not $pathExists) {
            try {
                $newUserPath = if ([string]::IsNullOrWhiteSpace($userPath)) { $InstallDir } else { "$userPath;$InstallDir" }
                [Environment]::SetEnvironmentVariable('Path', $newUserPath, 'User')
                Write-Host 'Added the install directory to your user PATH. Open a new terminal to use it.'
            }
            catch {
                Write-Warning "Installed successfully, but could not update the user PATH: $($_.Exception.Message)"
            }
        }
        else {
            Write-Host 'The install directory is already on your user PATH.'
        }

        $currentPathEntries = $env:Path -split ';'
        if (-not ($currentPathEntries | Where-Object { [string]::Equals($_.TrimEnd([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar), $normalizedInstallDir, [StringComparison]::OrdinalIgnoreCase) })) {
            $env:Path = "$InstallDir;$env:Path"
        }
    }
}
finally {
    if ($stagePath -and (Test-Path -LiteralPath $stagePath)) {
        Remove-Item -LiteralPath $stagePath -Force
    }
    if (Test-Path -LiteralPath $workDir) {
        Remove-Item -LiteralPath $workDir -Recurse -Force
    }
}
