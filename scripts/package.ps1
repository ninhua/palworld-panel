param(
  [string]$Version = "v0.0.0-dev",
  [switch]$SkipTests,
  [switch]$Clean,
  [string]$MingwGcc = "C:\msys64\mingw64\bin\gcc.exe",
  [string]$RuntimeRoot = ""
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
$ScriptDir = $PSScriptRoot
Import-Module (Join-Path $ScriptDir "windows-e2e-common.psm1") -Force
$RootDir = Get-PalPanelRepositoryRoot
$MingwGcc = Resolve-PalPanelPath -Path $MingwGcc -BasePath $RootDir
$MingwBin = Split-Path -Parent $MingwGcc
$MingwGxx = Join-Path $MingwBin "g++.exe"
if (-not (Test-Path -LiteralPath $MingwGcc -PathType Leaf)) {
  throw "MinGW gcc not found: $MingwGcc"
}
if (-not (Test-Path -LiteralPath $MingwGxx -PathType Leaf)) {
  throw "MinGW g++ not found next to gcc: $MingwGxx"
}
if ($Version -notmatch '^[A-Za-z0-9][A-Za-z0-9._+-]*$') {
  throw "Version contains unsafe path or shell characters: $Version"
}

$ManagedRuntimeRoot = if ([string]::IsNullOrWhiteSpace($RuntimeRoot)) {
  Join-Path $RootDir "dev-runtime\windows"
} else {
  Resolve-PalPanelPath -Path $RuntimeRoot -BasePath $RootDir
}
$ManagedRuntimeRoot = Initialize-PalPanelWindowsLayout -RepositoryRoot $RootDir -RuntimeRoot $ManagedRuntimeRoot
$PackagesDir = if ([string]::IsNullOrWhiteSpace($RuntimeRoot)) {
  Join-Path $RootDir "dist\packages"
} else {
  Join-Path $ManagedRuntimeRoot "package"
}
$PackageName = "palpanel_${Version}_windows_amd64"
$PackageDir = Join-Path $PackagesDir $PackageName
$Archive = Join-Path $PackagesDir "$PackageName.zip"
$WebUIEmbedDir = Join-Path $RootDir "backend\internal\webui\embedded"
$PalOpsMapStageDir = Join-Path $RootDir "frontend\public\map\palops"
$MapLibreStageDir = Join-Path $RootDir "frontend\public\vendor\maplibre-gl"
$DependencyMode = if ([string]::IsNullOrWhiteSpace($env:PALPANEL_DEPENDENCY_MODE)) { "online" } else { $env:PALPANEL_DEPENDENCY_MODE.ToLowerInvariant() }
$VendorRoot = if ([string]::IsNullOrWhiteSpace($env:PALPANEL_VENDOR_ROOT)) { Join-Path $RootDir ".vendor" } else { Resolve-PalPanelPath -Path $env:PALPANEL_VENDOR_ROOT -BasePath $RootDir }
$VendorWorkDir = Join-Path $ManagedRuntimeRoot "temp\vendor-$PID-$([guid]::NewGuid().ToString('N'))"
$VendorPrepared = $false
$NpmCiArguments = @("ci")
$DotnetRestoreArguments = @()
$PackageTemp = Join-Path $ManagedRuntimeRoot "temp\package-$PID-$([guid]::NewGuid().ToString('N'))"
Assert-PalPanelManagedPath -RepositoryRoot $RootDir -TargetPath $PackageTemp | Out-Null
New-Item -ItemType Directory -Force -Path $PackageTemp | Out-Null
$PreviousTemp = $env:TEMP
$PreviousTmp = $env:TMP
$PreviousGoCache = $env:GOCACHE
$PreviousNpmCache = $env:NPM_CONFIG_CACHE
$PreviousCargoTargetDir = $env:CARGO_TARGET_DIR
$env:TEMP = $PackageTemp
$env:TMP = $PackageTemp
$env:GOCACHE = Join-Path $PackageTemp "go-cache"
$env:NPM_CONFIG_CACHE = Join-Path $PackageTemp "npm-cache"
$env:CARGO_TARGET_DIR = Join-Path $PackageTemp "cargo-target"
New-Item -ItemType Directory -Force -Path $env:GOCACHE | Out-Null
New-Item -ItemType Directory -Force -Path $env:NPM_CONFIG_CACHE | Out-Null
$PackageSucceeded = $false

function Invoke-External {
  param([string]$FilePath, [string[]]$Arguments, [string]$WorkingDirectory)
  Push-Location $WorkingDirectory
  try {
    & $FilePath @Arguments
    if ($LASTEXITCODE -ne 0) {
      throw "$FilePath $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
  } finally {
    Pop-Location
  }
}

function Invoke-CargoWithTargetDirectory {
  param(
    [string[]]$Arguments,
    [string]$WorkingDirectory,
    [string]$TargetDirectory
  )

  $cargoCommand = Get-Command cargo.exe -ErrorAction SilentlyContinue
  if ($null -eq $cargoCommand) {
    throw "cargo.exe is required to build palworld-uid-remap.exe"
  }
  $previousCargoTargetDir = $env:CARGO_TARGET_DIR
  try {
    $env:CARGO_TARGET_DIR = $TargetDirectory
    Invoke-External $cargoCommand.Source $Arguments $WorkingDirectory
  } finally {
    $env:CARGO_TARGET_DIR = $previousCargoTargetDir
  }
}

function Invoke-GoTestsWithWindowsLockRetry {
  param([string]$WorkingDirectory, [int]$MaxAttempts = 3)

  for ($attempt = 1; $attempt -le $MaxAttempts; $attempt++) {
    Push-Location $WorkingDirectory
    $previousErrorActionPreference = $ErrorActionPreference
    try {
      # Windows PowerShell 5 promotes native stderr to NativeCommandError when
      # ErrorActionPreference is Stop. Capture all compiler output and decide
      # success from the native process exit code instead.
      $ErrorActionPreference = "Continue"
      $output = @(& go test -p=1 ./... 2>&1)
      $exitCode = $LASTEXITCODE
    } finally {
      $ErrorActionPreference = $previousErrorActionPreference
      Pop-Location
    }
    $output | ForEach-Object { Write-Host $_ }
    if ($exitCode -eq 0) {
      return
    }

    $outputText = ($output | Out-String)
    $isTransientWindowsLock = $outputText -match '(?i)process cannot access the file because it is being used by another process|TempDir RemoveAll cleanup|directory is not empty|unlinkat'
    if (-not $isTransientWindowsLock -or $attempt -eq $MaxAttempts) {
      throw "go test -p=1 ./... failed with exit code $exitCode"
    }

    Write-Warning "Windows temporarily locked a newly built Go test executable; retrying the full Go test suite ($attempt/$MaxAttempts)."
    Start-Sleep -Seconds 2
  }
}

function Clear-WebUIStage {
  if (-not (Test-Path $WebUIEmbedDir)) { return }
  Get-ChildItem -LiteralPath $WebUIEmbedDir -Force |
    Where-Object { $_.Name -ne ".keep" } |
    Remove-Item -Recurse -Force
}


function Clear-PalOpsMapStage {
  if (Test-Path -LiteralPath $PalOpsMapStageDir) {
    Remove-Item -LiteralPath $PalOpsMapStageDir -Recurse -Force
  }
}

function Clear-MapLibreStage {
  if (Test-Path -LiteralPath $MapLibreStageDir) {
    Remove-Item -LiteralPath $MapLibreStageDir -Recurse -Force
  }
}

function Clear-VendorStage {
  if ($VendorPrepared) {
    try {
      Invoke-External "python" @(
        (Join-Path $RootDir "scripts\vendorctl.py"),
        "cleanup",
        "--repository", $RootDir,
        "--work-root", $VendorWorkDir
      ) $RootDir
    } catch {
      Write-Warning "Unable to clean vendor staging: $($_.Exception.Message)"
    }
  } elseif (Test-Path -LiteralPath $VendorWorkDir) {
    Remove-Item -LiteralPath $VendorWorkDir -Recurse -Force
  }
}

function Initialize-DependencyMode {
  if ($DependencyMode -eq "online") { return }
  if ($DependencyMode -notin @("mirror", "offline")) {
    throw "PALPANEL_DEPENDENCY_MODE must be online, mirror, or offline"
  }
  if (-not (Test-Path -LiteralPath $VendorRoot -PathType Container)) {
    throw "Vendor root does not exist: $VendorRoot"
  }
  $profile = if ($DependencyMode -eq "offline") { "build" } else { "source" }
  Write-Host "[palpanel] Verifying dependency mirror ($profile)"
  Invoke-External "python" @((Join-Path $RootDir "scripts\vendorctl.py"), "verify", "--root", $VendorRoot, "--profile", $profile) $RootDir
  Invoke-External "python" @(
    (Join-Path $RootDir "scripts\vendorctl.py"),
    "prepare",
    "--root", $VendorRoot,
    "--repository", $RootDir,
    "--work-root", $VendorWorkDir,
    "--profile", $profile
  ) $RootDir
  $script:VendorPrepared = $true
  $environmentJson = & python (Join-Path $RootDir "scripts\vendorctl.py") env --root $VendorRoot --work-root $VendorWorkDir --format json
  if ($LASTEXITCODE -ne 0) { throw "vendorctl env failed with exit code $LASTEXITCODE" }
  $environment = ($environmentJson | Out-String) | ConvertFrom-Json
  foreach ($property in $environment.PSObject.Properties) {
    Set-Item -Path "Env:$($property.Name)" -Value ([string]$property.Value)
  }
  if ($DependencyMode -eq "offline") {
    $env:GOPROXY = "off"
    $env:GOSUMDB = "off"
    $env:CARGO_NET_OFFLINE = "true"
    $env:NPM_CONFIG_OFFLINE = "true"
    $env:DOTNET_RESTORE_IGNORE_FAILED_SOURCES = "true"
    $script:NpmCiArguments += "--offline"
    $script:DotnetRestoreArguments += "-p:RestoreIgnoreFailedSources=true"
  }
}

function Sync-MapLibreAssets {
  $arguments = @(
    (Join-Path $RootDir "scripts\sync_maplibre_assets.py"),
    "--destination", $MapLibreStageDir
  )
  if (-not [string]::IsNullOrWhiteSpace($env:PALPANEL_MAPLIBRE_SOURCE_DIR)) {
    $arguments += @("--source-dir", $env:PALPANEL_MAPLIBRE_SOURCE_DIR)
  } else {
    $arguments += "--allow-network"
  }
  Write-Host "[palpanel] Synchronizing pinned MapLibre runtime"
  Invoke-External "python" $arguments $RootDir
}

function Sync-PalOpsMapAssets {
  $repository = if ([string]::IsNullOrWhiteSpace($env:PALPANEL_MAP_ASSETS_REPOSITORY)) { "ninhua/palpanel-assets" } else { $env:PALPANEL_MAP_ASSETS_REPOSITORY }
  $ref = if ([string]::IsNullOrWhiteSpace($env:PALPANEL_MAP_ASSETS_REF)) { "main" } else { $env:PALPANEL_MAP_ASSETS_REF }
  $sourceDir = if (-not [string]::IsNullOrWhiteSpace($env:PALPANEL_MAP_ASSETS_SOURCE_DIR)) { $env:PALPANEL_MAP_ASSETS_SOURCE_DIR } else { $env:PALPANEL_PALOPS_MAP_SOURCE_DIR }
  $archive = if (-not [string]::IsNullOrWhiteSpace($env:PALPANEL_MAP_ASSETS_ARCHIVE)) { $env:PALPANEL_MAP_ASSETS_ARCHIVE } else { $env:PALPANEL_PALOPS_MAP_ARCHIVE }
  $tileSource = if (-not [string]::IsNullOrWhiteSpace($env:PALPANEL_MAP_ASSETS_TILE_SOURCE_DIR)) { $env:PALPANEL_MAP_ASSETS_TILE_SOURCE_DIR } else { $env:PALPANEL_PALOPS_TILE_SOURCE_DIR }
  $arguments = @(
    (Join-Path $RootDir "scripts\sync_palops_map_assets.py"),
    "--destination", $PalOpsMapStageDir,
    "--repository", $repository,
    "--ref", $ref
  )
  if (-not [string]::IsNullOrWhiteSpace($sourceDir)) {
    $arguments += @("--source-dir", $sourceDir)
  } elseif (-not [string]::IsNullOrWhiteSpace($archive)) {
    $arguments += @("--archive", $archive)
  } else {
    $arguments += "--allow-network"
  }
  if (-not [string]::IsNullOrWhiteSpace($tileSource)) {
    $arguments += @("--tiles-source-dir", $tileSource)
  }
  if ($env:PALPANEL_ALLOW_MISSING_MAP_TILES -eq "true") {
    $arguments += "--allow-missing-tiles"
  }
  $releaseTag = if ([string]::IsNullOrWhiteSpace($env:PALPANEL_MAP_ASSETS_RELEASE_TAG)) { "latest" } else { $env:PALPANEL_MAP_ASSETS_RELEASE_TAG }
  Write-Host "[palpanel] Synchronizing PalPanel map resources from Release $releaseTag (repository $repository@$ref)"
  Invoke-External "python" $arguments $RootDir
}

function Invoke-GoBuildWithWindowsLockRetry {
  param([string[]]$Arguments, [string]$WorkingDirectory, [int]$MaxAttempts = 5)

  for ($attempt = 1; $attempt -le $MaxAttempts; $attempt++) {
    Push-Location $WorkingDirectory
    $previousErrorActionPreference = $ErrorActionPreference
    try {
      $ErrorActionPreference = "Continue"
      $output = @(& go @Arguments 2>&1)
      $exitCode = $LASTEXITCODE
    } finally {
      $ErrorActionPreference = $previousErrorActionPreference
      Pop-Location
    }
    $output | ForEach-Object { Write-Host $_ }
    if ($exitCode -eq 0) {
      return
    }

    $outputText = ($output | Out-String)
    $isTransientWindowsLock = $outputText -match '(?i)access is denied|being used by another process|process cannot access the file'
    if (-not $isTransientWindowsLock -or $attempt -eq $MaxAttempts) {
      throw "go $($Arguments -join ' ') failed with exit code $exitCode"
    }
    Write-Warning "Windows temporarily locked a Go build artifact; retrying ($attempt/$MaxAttempts)."
    Start-Sleep -Seconds 2
  }
}

function Get-FileSHA256WithWindowsLockRetry {
  param([Parameter(Mandatory = $true)][string]$Path, [int]$MaxAttempts = 8)

  for ($attempt = 1; $attempt -le $MaxAttempts; $attempt++) {
    try {
      return (Get-FileHash -LiteralPath $Path -Algorithm SHA256 -ErrorAction Stop).Hash.ToLowerInvariant()
    } catch {
      $isTransientWindowsLock = $_.Exception.Message -match '(?i)being used by another process|process cannot access the file'
      if (-not $isTransientWindowsLock -or $attempt -eq $MaxAttempts) {
        throw
      }
      Start-Sleep -Milliseconds (250 * $attempt)
    }
  }
}

function Remove-PackageTempWithRetry {
  param([int]$MaxAttempts = 5)

  if (-not (Test-Path -LiteralPath $PackageTemp)) { return }
  for ($attempt = 1; $attempt -le $MaxAttempts; $attempt++) {
    try {
      Remove-PalPanelManagedDirectory -RepositoryRoot $RootDir -RuntimeRoot $ManagedRuntimeRoot -TargetPath $PackageTemp
      return
    } catch {
      if ($attempt -eq $MaxAttempts) {
        Write-Warning "Package succeeded, but the temporary directory is still locked and was retained: $PackageTemp ($($_.Exception.Message))"
        return
      }
      Write-Warning "Temporary build files are still locked; retrying cleanup ($attempt/$MaxAttempts)."
      Start-Sleep -Seconds 2
    }
  }
}

try {
  $commitOutput = & git -C $RootDir rev-parse HEAD
  if ($LASTEXITCODE -ne 0) {
    throw "git rev-parse HEAD failed with exit code $LASTEXITCODE"
  }
  $Commit = ($commitOutput | Out-String).Trim()
  if ([string]::IsNullOrWhiteSpace($Commit)) {
    throw "git rev-parse HEAD returned an empty commit"
  }
  $BuildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

  if (-not [string]::IsNullOrWhiteSpace($RuntimeRoot)) {
    Assert-PalPanelManagedPath -RepositoryRoot $RootDir -TargetPath $PackageDir | Out-Null
    Assert-PalPanelManagedPath -RepositoryRoot $RootDir -TargetPath $Archive | Out-Null
  }
  if (Test-Path -LiteralPath $PackageDir) {
    if (-not $Clean) {
      throw "Package directory already exists: $PackageDir. Rerun with -Clean to replace it safely."
    }
    Remove-Item -LiteralPath $PackageDir -Recurse -Force
  }
  if (Test-Path $Archive) {
    Remove-Item -Force $Archive
  }
  New-Item -ItemType Directory -Force -Path $PackageDir | Out-Null

Initialize-DependencyMode
Sync-PalOpsMapAssets
Sync-MapLibreAssets

if (-not $SkipTests) {
  Invoke-GoTestsWithWindowsLockRetry (Join-Path $RootDir "backend")
  Invoke-External "cargo.exe" @("test", "--locked") (Join-Path $RootDir "tools\palworld-uid-remap")
  $oldCgo = $env:CGO_ENABLED
  $oldCc = $env:CC
  $oldCxx = $env:CXX
  $oldPath = $env:PATH
  try {
    $env:CGO_ENABLED = "1"
    $env:CC = $MingwGcc
    $env:CXX = $MingwGxx
    $env:PATH = $MingwBin + [System.IO.Path]::PathSeparator + $oldPath
    Invoke-GoTestsWithWindowsLockRetry (Join-Path $RootDir "sav-cli")
  } finally {
    $env:CGO_ENABLED = $oldCgo
    $env:CC = $oldCc
    $env:CXX = $oldCxx
    $env:PATH = $oldPath
  }
  Write-Host "[palpanel] Running UID remapper tests"
  Invoke-CargoWithTargetDirectory `
    -Arguments @("test", "--locked") `
    -WorkingDirectory (Join-Path $RootDir "tools\palworld-uid-remap") `
    -TargetDirectory (Join-Path $PackageTemp "uid-remapper-tests")
  Invoke-External "npm.cmd" $NpmCiArguments (Join-Path $RootDir "frontend")
  # CI checks that generated contracts are committed. Local packaging must also
  # work before a commit, so run the same validation without diffing against HEAD.
  Invoke-External "npm.cmd" @("run", "generate:api-types") (Join-Path $RootDir "frontend")
  Invoke-External "npm.cmd" @("run", "typecheck") (Join-Path $RootDir "frontend")
  Invoke-External "npm.cmd" @("run", "lint") (Join-Path $RootDir "frontend")
  Invoke-External "npm.cmd" @("run", "test") (Join-Path $RootDir "frontend")
  Invoke-External "npm.cmd" @("run", "build") (Join-Path $RootDir "frontend")
} else {
  Invoke-External "npm.cmd" $NpmCiArguments (Join-Path $RootDir "frontend")
  Invoke-External "npm.cmd" @("run", "build") (Join-Path $RootDir "frontend")
}

New-Item -ItemType Directory -Force -Path (Join-Path $PackageDir "backend\deployments") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $PackageDir "config") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $PackageDir "licenses") | Out-Null
Copy-Item -Recurse -Force (Join-Path $RootDir "backend\deployments\wine-runner") (Join-Path $PackageDir "backend\deployments\wine-runner")
Copy-Item -Force (Join-Path $RootDir "scripts\palpanel.env.example") (Join-Path $PackageDir "config\palpanel.env.example")
Copy-Item -Force (Join-Path $RootDir "scripts\package-README-windows.md") (Join-Path $PackageDir "README.md")
New-Item -ItemType Directory -Force -Path (Join-Path $PackageDir "maintenance") | Out-Null
foreach ($maintenanceScript in @(
  "windows-maintenance.psm1",
  "upgrade-windows.ps1",
  "uninstall-windows.ps1",
  "recover-windows-config.ps1"
)) {
  Copy-Item -Force (Join-Path $RootDir (Join-Path "scripts" $maintenanceScript)) (Join-Path $PackageDir (Join-Path "maintenance" $maintenanceScript))
}
Copy-Item -Force (Join-Path $RootDir "LICENSE") (Join-Path $PackageDir "LICENSE")
Copy-Item -Force (Join-Path $RootDir "THIRD_PARTY_LICENSES.txt") (Join-Path $PackageDir "THIRD_PARTY_LICENSES.txt")
Copy-Item -Force (Join-Path $RootDir "sav-cli\LICENSE") (Join-Path $PackageDir "licenses\sav-cli-LICENSE.txt")
Copy-Item -Force (Join-Path $RootDir "third_party\palcalc\LICENSE.txt") (Join-Path $PackageDir "licenses\PalCalc-MIT.txt")
Copy-Item -Force (Join-Path $RootDir "backend\internal\pallocalize\LICENSE.apache-2.0") (Join-Path $PackageDir "licenses\pallocalize-Apache-2.0.txt")
Copy-Item -Force (Join-Path $RootDir "backend\internal\paldefender\assets\LICENSE.txt") (Join-Path $PackageDir "licenses\PalDefender-MIT.txt")

$uidRemapperTarget = "x86_64-pc-windows-msvc"
$uidRemapperBuildDir = Join-Path $PackageTemp "uid-remapper-windows-amd64"
Write-Host "[palpanel] Building UID remapper windows-amd64"
Invoke-CargoWithTargetDirectory `
  -Arguments @("build", "--locked", "--release", "--target", $uidRemapperTarget) `
  -WorkingDirectory (Join-Path $RootDir "tools\palworld-uid-remap") `
  -TargetDirectory $uidRemapperBuildDir
$uidRemapperSource = Join-Path $uidRemapperBuildDir "$uidRemapperTarget\release\palworld-uid-remap.exe"
if (-not (Test-Path -LiteralPath $uidRemapperSource -PathType Leaf)) {
  throw "UID remapper build output is missing: $uidRemapperSource"
}
$uidRemapperDestination = Join-Path $PackageDir "palworld-uid-remap.exe"
Copy-Item -Force $uidRemapperSource $uidRemapperDestination
$uidRemapperSHA256 = Get-FileSHA256WithWindowsLockRetry -Path $uidRemapperDestination
if ($uidRemapperSHA256 -notmatch '^[0-9a-f]{64}$') {
  throw "Unable to calculate UID remapper SHA-256"
}

$backendLdflags = "-s -w -X palpanel/internal/buildinfo.Version=$Version -X palpanel/internal/buildinfo.Commit=$Commit -X palpanel/internal/buildinfo.BuildTime=$BuildTime"
$palpanelLdflags = "$backendLdflags -X palpanel/internal/api.hostMigrationHelperSHA256=$uidRemapperSHA256"
$savLdflags = "-s -w -X palpanel/sav-cli/internal/buildinfo.Version=$Version -X palpanel/sav-cli/internal/buildinfo.Commit=$Commit -X palpanel/sav-cli/internal/buildinfo.BuildTime=$BuildTime"
$oldGoos = $env:GOOS
$oldGoarch = $env:GOARCH
$oldCgo = $env:CGO_ENABLED
$oldCc = $env:CC
$oldCxx = $env:CXX
$oldPath = $env:PATH
Clear-WebUIStage
New-Item -ItemType Directory -Force -Path $WebUIEmbedDir | Out-Null
Copy-Item -Recurse -Force (Join-Path $RootDir "frontend\dist\*") $WebUIEmbedDir
Clear-PalOpsMapStage
Clear-MapLibreStage
try {
  $env:GOOS = "windows"
  $env:GOARCH = "amd64"
  $env:CGO_ENABLED = "0"
  Invoke-GoBuildWithWindowsLockRetry -Arguments @("build", "-tags", "embed_webui", "-trimpath", "-ldflags", $palpanelLdflags, "-o", (Join-Path $PackageDir "palpanel-server.exe"), "./cmd/palpanel") -WorkingDirectory (Join-Path $RootDir "backend")
  Invoke-GoBuildWithWindowsLockRetry -Arguments @("build", "-trimpath", "-ldflags", "$backendLdflags -H windowsgui", "-o", (Join-Path $PackageDir "PalPanel.exe"), "./cmd/palpanel-launcher") -WorkingDirectory (Join-Path $RootDir "backend")
  Invoke-External "cargo.exe" @("build", "--locked", "--release") (Join-Path $RootDir "tools\palworld-uid-remap")
  Copy-Item -Force (Join-Path $env:CARGO_TARGET_DIR "release\palworld-uid-remap.exe") (Join-Path $PackageDir "palworld-uid-remap.exe")

  $env:CGO_ENABLED = "1"
  $env:CC = $MingwGcc
  $env:CXX = $MingwGxx
  $env:PATH = $MingwBin + [System.IO.Path]::PathSeparator + $oldPath
  Invoke-GoBuildWithWindowsLockRetry -Arguments @("build", "-trimpath", "-ldflags", $savLdflags, "-o", (Join-Path $PackageDir "sav-cli.exe"), "./cmd/sav_cli") -WorkingDirectory (Join-Path $RootDir "sav-cli")
  Invoke-External (Join-Path $PackageDir "sav-cli.exe") @("verify-build", "--require-oodle") $PackageDir
  $palcalcPublish = Join-Path $RootDir "dist\palcalc-win-x64"
  if (Test-Path $palcalcPublish) { Remove-Item -Recurse -Force $palcalcPublish }
  $dotnetPublishArguments = @(
    "publish", (Join-Path $RootDir "palcalc-bridge\PalCalc.Bridge.csproj"),
    "-c", "Release", "-r", "win-x64", "--self-contained", "true",
    "-p:PublishSingleFile=true", "-p:IncludeNativeLibrariesForSelfExtract=true",
    "-p:UseSharedCompilation=false"
  ) + $DotnetRestoreArguments + @("-o", $palcalcPublish)
  Invoke-External "dotnet" $dotnetPublishArguments $RootDir
  Copy-Item -Force (Join-Path $palcalcPublish "palcalc-bridge.exe") (Join-Path $PackageDir "palcalc-bridge.exe")
} finally {
  $env:GOOS = $oldGoos
  $env:GOARCH = $oldGoarch
  $env:CGO_ENABLED = $oldCgo
  $env:CC = $oldCc
  $env:CXX = $oldCxx
  $env:PATH = $oldPath
  Clear-WebUIStage
}

$checksumLines = Get-ChildItem -LiteralPath $PackageDir -Recurse -File |
  Where-Object { $_.Name -ne "checksums.txt" } |
  Sort-Object FullName |
  ForEach-Object {
    $relative = $_.FullName.Substring($PackageDir.Length).TrimStart("\") -replace "\\", "/"
    $hash = Get-FileSHA256WithWindowsLockRetry -Path $_.FullName
    "$hash  ./$relative"
  }
Set-Content -LiteralPath (Join-Path $PackageDir "checksums.txt") -Value $checksumLines -Encoding ASCII
Compress-Archive -Path $PackageDir -DestinationPath $Archive -Force
  Write-Host "[palpanel] Wrote unsigned Windows release package $Archive"
  $PackageSucceeded = $true
} finally {
  Clear-PalOpsMapStage
  Clear-MapLibreStage
  Clear-VendorStage
  $env:TEMP = $PreviousTemp
  $env:TMP = $PreviousTmp
  $env:GOCACHE = $PreviousGoCache
  $env:NPM_CONFIG_CACHE = $PreviousNpmCache
  $env:CARGO_TARGET_DIR = $PreviousCargoTargetDir
  if ($PackageSucceeded -and (Test-Path -LiteralPath $PackageTemp)) {
    $previousErrorActionPreference = $ErrorActionPreference
    try {
      $ErrorActionPreference = "Continue"
      & dotnet build-server shutdown 2>&1 | ForEach-Object { Write-Host $_ }
    } finally {
      $ErrorActionPreference = $previousErrorActionPreference
    }
    Remove-PackageTempWithRetry
  } elseif (-not $PackageSucceeded) {
    Write-Warning "Package staging was retained for diagnosis: $PackageTemp"
  }
}
