param(
  [Parameter(Mandatory = $true)]
  [string]$UE4SSRoot,
  [string]$OutputDir = (Join-Path $PSScriptRoot "build"),
  [string]$RustCompiler = "",
  [string]$BuildConfiguration = "Game__Shipping__Win64"
)

$ErrorActionPreference = "Stop"
$resolvedSDK = (Resolve-Path -LiteralPath $UE4SSRoot).Path
if (-not (Test-Path -LiteralPath (Join-Path $resolvedSDK "CMakeLists.txt") -PathType Leaf)) {
  throw "UE4SSRoot must point to a complete RE-UE4SS v3.0.1 source checkout"
}

$cmakeArgs = @(
  "-S", $PSScriptRoot,
  "-B", $OutputDir,
  "-A", "x64",
  "-DPALPANEL_UE4SS_ROOT=$resolvedSDK"
)
if ($RustCompiler) {
  $resolvedRust = (Resolve-Path -LiteralPath $RustCompiler).Path
  $cmakeArgs += "-DRust_COMPILER=$resolvedRust"
}

cmake @cmakeArgs
if ($LASTEXITCODE -ne 0) { throw "PalPanelBridge CMake configure failed" }

cmake --build $OutputDir --config $BuildConfiguration --target PalPanelBridge
if ($LASTEXITCODE -ne 0) { throw "PalPanelBridge build failed" }

$artifact = Join-Path $OutputDir "artifact\PalPanelBridge"
Copy-Item -Force (Join-Path $PSScriptRoot "config.ini.example") (Join-Path $artifact "config.ini.example")
Copy-Item -Force (Join-Path $PSScriptRoot "README.md") (Join-Path $artifact "README.md")
Write-Host "PalPanelBridge artifact: $artifact"
