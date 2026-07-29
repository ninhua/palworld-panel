param(
  [Parameter(Mandatory = $true)]
  [string]$UE4SSRoot,
  [string]$OutputDir = (Join-Path $PSScriptRoot "build"),
  [string]$RustCompiler = "",
  [string]$BuildConfiguration = "Game__Shipping__Win64",
  [string]$Generator = "Ninja"
)

$ErrorActionPreference = "Stop"
$expectedSDKCommit = "c838a8acaade1a0f860bdf249f039e58f4e10088"
$resolvedSDK = (Resolve-Path -LiteralPath $UE4SSRoot).Path
$sdkCMake = Join-Path $resolvedSDK "CMakeLists.txt"
if (-not (Test-Path -LiteralPath $sdkCMake -PathType Leaf)) {
  throw "UE4SSRoot must point to the complete experimental UE4SS source checkout"
}
if (-not (Test-Path -LiteralPath (Join-Path $resolvedSDK "deps\first\Unreal\CMakeLists.txt") -PathType Leaf)) {
  throw "UE4SSRoot is missing its pinned UEPseudo submodule"
}

$actualSDKCommit = (& git -C $resolvedSDK rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $actualSDKCommit -ne $expectedSDKCommit) {
  throw "PalPanelBridge requires experimental UE4SS commit $expectedSDKCommit; found $actualSDKCommit"
}

$resolvedOutput = [System.IO.Path]::GetFullPath($OutputDir)
$bridgeSource = $PSScriptRoot.Replace("\", "/")
$bridgeBuild = (Join-Path $resolvedOutput "palpanel-bridge").Replace("\", "/")
$utf8 = [System.Text.UTF8Encoding]::new($false)
$originalSDKCMake = [System.IO.File]::ReadAllText($sdkCMake, $utf8)
$compilerSettingsMarker = "# Apply compiler settings to all targets"
if (-not $originalSDKCMake.Contains($compilerSettingsMarker)) {
  throw "Unexpected experimental UE4SS root CMake layout"
}
$injectedSDKCMake = $originalSDKCMake.Replace(
  $compilerSettingsMarker,
  "add_subdirectory(`"$bridgeSource`" `"$bridgeBuild`")`n`n$compilerSettingsMarker"
)

$cmakeArgs = @(
  "-S", $resolvedSDK,
  "-B", $resolvedOutput,
  "-G", $Generator,
  "-DCMAKE_BUILD_TYPE=$BuildConfiguration"
)
if ($RustCompiler) {
  $resolvedRust = (Resolve-Path -LiteralPath $RustCompiler).Path
  $cmakeArgs += "-DRust_COMPILER=$resolvedRust"
}

try {
  [System.IO.File]::WriteAllText($sdkCMake, $injectedSDKCMake, $utf8)
  cmake @cmakeArgs
  if ($LASTEXITCODE -ne 0) { throw "PalPanelBridge CMake configure failed" }

  cmake --build $resolvedOutput --target PalPanelBridge
  if ($LASTEXITCODE -ne 0) { throw "PalPanelBridge build failed" }
} finally {
  [System.IO.File]::WriteAllText($sdkCMake, $originalSDKCMake, $utf8)
}

$artifact = Join-Path $resolvedOutput "artifact\PalPanelBridge"
Copy-Item -Force (Join-Path $PSScriptRoot "config.ini.example") (Join-Path $artifact "config.ini.example")
Copy-Item -Force (Join-Path $PSScriptRoot "README.md") (Join-Path $artifact "README.md")
Write-Host "PalPanelBridge artifact: $artifact"
