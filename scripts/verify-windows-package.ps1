param(
  [Parameter(Mandatory = $true)]
  [string]$Archive,
  [string]$ObjdumpPath = "",
  [string]$RuntimeRoot = "",
  [switch]$KeepArtifacts
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
Add-Type -AssemblyName System.IO.Compression.FileSystem
$ScriptDir = $PSScriptRoot
Import-Module (Join-Path $ScriptDir "windows-e2e-common.psm1") -Force
$RepositoryRoot = Get-PalPanelRepositoryRoot
if ([string]::IsNullOrWhiteSpace($RuntimeRoot)) {
  $RuntimeRoot = Join-Path $RepositoryRoot "dev-runtime\windows"
} else {
  $RuntimeRoot = Resolve-PalPanelPath -Path $RuntimeRoot -BasePath $RepositoryRoot
}
$RuntimeRoot = Initialize-PalPanelWindowsLayout -RepositoryRoot $RepositoryRoot -RuntimeRoot $RuntimeRoot
$Archive = Resolve-PalPanelPath -Path $Archive -BasePath $RepositoryRoot
if (-not (Test-Path -LiteralPath $Archive -PathType Leaf)) {
  throw "Windows archive is missing: $Archive"
}
$PackageName = [System.IO.Path]::GetFileNameWithoutExtension($Archive)
$Temp = Join-Path $RuntimeRoot "temp\palpanel-verify-$([guid]::NewGuid().ToString('N'))"
Assert-PalPanelManagedPath -RepositoryRoot $RepositoryRoot -TargetPath $Temp | Out-Null
$PreviousTemp = $env:TEMP
$PreviousTmp = $env:TMP
$env:TEMP = Join-Path $RuntimeRoot "temp"
$env:TMP = $env:TEMP
$Succeeded = $false

try {
  $zip = [System.IO.Compression.ZipFile]::OpenRead($Archive)
  try {
    if ($zip.Entries.Count -gt 200000) {
      throw "archive contains too many entries"
    }
    $seen = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::OrdinalIgnoreCase)
    [int64]$declaredBytes = 0
    foreach ($entry in $zip.Entries) {
      $normalized = $entry.FullName.Replace('\', '/').TrimEnd('/')
      if ([string]::IsNullOrWhiteSpace($normalized)) { continue }
      if ($normalized.StartsWith('/') -or $normalized.Contains(':')) {
        throw "archive contains an unsafe absolute path: $($entry.FullName)"
      }
      $components = $normalized.Split('/')
      if ($components -contains "" -or $components -contains "." -or $components -contains "..") {
        throw "archive contains an unsafe path component: $($entry.FullName)"
      }
      foreach ($component in $components) {
        if ($component.EndsWith('.') -or $component.EndsWith(' ') -or $component.IndexOfAny([char[]]'<>"|?*') -ge 0) {
          throw "archive contains a Windows-unsafe path component: $($entry.FullName)"
        }
        foreach ($character in $component.ToCharArray()) {
          if ([int]$character -lt 0x20 -or [int]$character -eq 0x7f) {
            throw "archive contains a control character in a path: $($entry.FullName)"
          }
        }
        $deviceBase = $component.Split('.')[0].ToUpperInvariant()
        if ($deviceBase -in @('CON', 'PRN', 'AUX', 'NUL', 'CLOCK$') -or $deviceBase -match '^(COM|LPT)[1-9]$') {
          throw "archive contains a reserved Windows device path: $($entry.FullName)"
        }
      }
      if (-not $seen.Add($normalized)) {
        throw "archive contains a duplicate case-insensitive path: $($entry.FullName)"
      }
      if ($entry.Length -gt 64GB -or $declaredBytes -gt (64GB - $entry.Length)) {
        throw "archive exceeds the extracted size limit"
      }
      $declaredBytes += $entry.Length
      $unixType = (($entry.ExternalAttributes -shr 16) -band 0xF000)
      if ($unixType -eq 0xA000) {
        throw "archive contains a symbolic link: $($entry.FullName)"
      }
      $target = [System.IO.Path]::GetFullPath((Join-Path $Temp ($normalized.Replace('/', [System.IO.Path]::DirectorySeparatorChar))))
      if (-not (Test-PalPanelPathWithin -Root $Temp -Target $target)) {
        throw "archive path escapes extraction root: $($entry.FullName)"
      }
    }
  } finally {
    $zip.Dispose()
  }

  Expand-Archive -LiteralPath $Archive -DestinationPath $Temp
  $Package = Join-Path $Temp $PackageName
  if (-not (Test-Path -LiteralPath $Package -PathType Container)) {
    throw "archive package root is missing: $PackageName"
  }

  $required = @(
    "PalPanel.exe",
    "palpanel-server.exe",
    "sav-cli.exe",
    "palcalc-bridge.exe",
    "palworld-uid-remap.exe",
    "config\palpanel.env.example",
    "LICENSE",
    "THIRD_PARTY_LICENSES.txt",
    "licenses\sav-cli-LICENSE.txt",
    "licenses\pallocalize-Apache-2.0.txt",
    "licenses\PalDefender-MIT.txt",
    "licenses\PalCalc-MIT.txt",
    "maintenance\windows-maintenance.psm1",
    "maintenance\upgrade-windows.ps1",
    "maintenance\uninstall-windows.ps1",
    "maintenance\recover-windows-config.ps1",
    "checksums.txt"
  )
  foreach ($relative in $required) {
    if (-not (Test-Path -LiteralPath (Join-Path $Package $relative) -PathType Leaf)) {
      throw "Windows package is missing $relative"
    }
  }

  foreach ($runtimePath in @("data", "logs", "run", "config\palpanel.env")) {
    if (Test-Path -LiteralPath (Join-Path $Package $runtimePath)) {
      throw "Windows package contains runtime path $runtimePath"
    }
  }
  if (Test-Path -LiteralPath (Join-Path $Package "frontend")) {
    throw "Windows package must not contain a separate frontend directory"
  }

  $checksumPath = Join-Path $Package "checksums.txt"
  $checksumLines = Get-Content -LiteralPath $checksumPath
  $verified = 0
  foreach ($line in $checksumLines) {
    if ($line -notmatch '^([0-9a-fA-F]{64})  \./(.+)$') {
      throw "invalid checksum line: $line"
    }
    $expected = $Matches[1].ToLowerInvariant()
    $relative = $Matches[2].Replace('/', [System.IO.Path]::DirectorySeparatorChar)
    $path = Join-Path $Package $relative
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
      throw "checksummed file is missing: $relative"
    }
    $actual = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
      throw "checksum mismatch: $relative"
    }
    $verified++
  }
  $packagedFiles = @(Get-ChildItem -LiteralPath $Package -Recurse -File | Where-Object { $_.Name -ne "checksums.txt" })
  if ($verified -ne $packagedFiles.Count) {
    throw "checksums.txt covers $verified files, package contains $($packagedFiles.Count) files"
  }

  if ($ObjdumpPath) {
    $ObjdumpPath = Resolve-PalPanelPath -Path $ObjdumpPath -BasePath $RepositoryRoot
    if (-not (Test-Path -LiteralPath $ObjdumpPath -PathType Leaf)) {
      throw "objdump is missing: $ObjdumpPath"
    }
    $imports = (& $ObjdumpPath -p (Join-Path $Package "sav-cli.exe") | Out-String)
    if ($LASTEXITCODE -ne 0) {
      throw "objdump failed with exit code $LASTEXITCODE"
    }
    if ($imports -match '(?i)libgcc_s[^\s]*\.dll|libstdc\+\+-6\.dll|libwinpthread-1\.dll') {
      throw "sav-cli.exe depends on a MinGW runtime DLL"
    }
  }

  Write-Host "Windows package content verification passed"
  $Succeeded = $true
} finally {
  $env:TEMP = $PreviousTemp
  $env:TMP = $PreviousTmp
  if ($Succeeded -and -not $KeepArtifacts) {
    Remove-PalPanelManagedDirectory -RepositoryRoot $RepositoryRoot -RuntimeRoot $RuntimeRoot -TargetPath $Temp
  } elseif (Test-Path -LiteralPath $Temp) {
    Write-Warning "Package verification extraction retained at $Temp"
  }
}
