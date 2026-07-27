[CmdletBinding()]
param(
  # Optional full paths make the script usable with portable toolchains and
  # with PowerShell execution policies that block npm.ps1.  npm.cmd is always
  # preferred when it is available.
  [string]$GoPath = '',
  [string]$NpmPath = ''
)

$ErrorActionPreference = 'Stop'

$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path

function Resolve-Tool {
  param(
    [string]$Requested,
    [string[]]$CommandNames,
    [string[]]$Fallbacks,
    [string]$DisplayName
  )

  # An explicitly supplied path is authoritative.  Falling back silently
  # would make a typo appear to work while building with a different
  # toolchain than the caller selected.
  if ($Requested -and $Requested.Trim()) {
    $resolved = $Requested.Trim()
    if (-not [IO.Path]::IsPathRooted($resolved)) {
      $command = Get-Command $resolved -ErrorAction SilentlyContinue | Select-Object -First 1
      if ($command -and $command.Source) {
        $resolved = $command.Source
      }
    }
    if (-not (Test-Path -LiteralPath $resolved -PathType Leaf)) {
      throw "$DisplayName path was not found: $Requested"
    }
    if ([IO.Path]::GetFileName($resolved) -ieq 'npm.ps1') {
      $cmdSibling = Join-Path ([IO.Path]::GetDirectoryName($resolved)) 'npm.cmd'
      if (Test-Path -LiteralPath $cmdSibling -PathType Leaf) {
        $resolved = $cmdSibling
      }
    }
    return (Resolve-Path -LiteralPath $resolved).Path
  }

  $candidates = @()

  foreach ($name in $CommandNames) {
    $command = Get-Command $name -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($command -and $command.Source) {
      $candidates += $command.Source
    }
  }
  $candidates += $Fallbacks

  foreach ($candidate in ($candidates | Select-Object -Unique)) {
    $resolved = $candidate
    if (-not [IO.Path]::IsPathRooted($resolved)) {
      $command = Get-Command $resolved -ErrorAction SilentlyContinue | Select-Object -First 1
      if ($command -and $command.Source) {
        $resolved = $command.Source
      }
    }
    if (-not (Test-Path -LiteralPath $resolved -PathType Leaf)) {
      continue
    }

    # On Windows, invoking npm.ps1 can be rejected by the execution policy.
    # Use the sibling npm.cmd whenever one exists.
    if ([IO.Path]::GetFileName($resolved) -ieq 'npm.ps1') {
      $cmdSibling = Join-Path ([IO.Path]::GetDirectoryName($resolved)) 'npm.cmd'
      if (Test-Path -LiteralPath $cmdSibling -PathType Leaf) {
        $resolved = $cmdSibling
      }
    }
    return (Resolve-Path -LiteralPath $resolved).Path
  }

  throw "$DisplayName was not found. Install the required tool or pass -$DisplayName`Path explicitly."
}

$go = Resolve-Tool -Requested $GoPath -CommandNames @('go.exe', 'go') -Fallbacks @(
  'C:\Program Files\Go\bin\go.exe',
  'C:\Program Files (x86)\Go\bin\go.exe'
) -DisplayName 'Go'
$npm = Resolve-Tool -Requested $NpmPath -CommandNames @('npm.cmd', 'npm') -Fallbacks @(
  'C:\Program Files\nodejs\npm.cmd',
  (Join-Path $env:LOCALAPPDATA 'Programs\nodejs\npm.cmd')
) -DisplayName 'npm'

# Wails invokes the frontend commands itself.  Put the directory containing
# npm.cmd (and node.exe) first so those child processes resolve the same
# toolchain selected above.
$npmDir = Split-Path -Parent $npm
if ($npmDir) {
  $env:Path = "$npmDir;$env:Path"
}

$source = Join-Path $root 'assets\mediainfo'
foreach ($required in @('MediaInfo.exe', 'LIBCURL.DLL', 'LICENSE', 'License.html', 'CURL-LICENSE', 'README.md')) {
  $requiredPath = Join-Path $source $required
  if (-not (Test-Path -LiteralPath $requiredPath -PathType Leaf)) {
    throw "Bundled MediaInfo asset is missing: $requiredPath"
  }
}

Push-Location $root
try {
  # Strip local build-machine paths from the distributable executable.
  & $go run github.com/wailsapp/wails/v2/cmd/wails@v2.12.0 build -trimpath
  if ($LASTEXITCODE -ne 0) { throw "Wails build failed with exit code $LASTEXITCODE." }
} finally {
  Pop-Location
}

$target = Join-Path $root 'build\bin\assets\mediainfo'
New-Item -ItemType Directory -Force -Path $target | Out-Null
# Copy every bundled file, not just today's two DLL/EXE files.  Future
# MediaInfo distributions may add a dependency that is required at runtime.
Get-ChildItem -LiteralPath $source -File | ForEach-Object {
  Copy-Item -LiteralPath $_.FullName -Destination (Join-Path $target $_.Name) -Force
}

$output = Join-Path $root 'build\bin\vmf-preupload.exe'
if (-not (Test-Path -LiteralPath $output -PathType Leaf)) {
  throw "Wails completed without producing $output"
}

# Older development commands wrote a non-Wails binary directly to build\.
# Remove that exact legacy artifact after a successful package build so users
# cannot accidentally launch a stale executable instead of build\bin\.
$legacyOutput = Join-Path $root 'build\vmf-preupload.exe'
if (Test-Path -LiteralPath $legacyOutput -PathType Leaf) {
  Remove-Item -LiteralPath $legacyOutput -Force
  Write-Host "Removed stale legacy artifact: $legacyOutput"
}

Write-Host "Built: $output"
Write-Host "MediaInfo runtime assets: $target"
