param(
  [Parameter(Mandatory = $true)]
  [string]$UE4SSRoot,
  [string]$OutputDir = (Join-Path $PSScriptRoot "build"),
  [string]$RustCompiler = "",
  [string]$BuildConfiguration = "Game__Shipping__Win64",
  [string]$Generator = "Ninja"
)

$ErrorActionPreference = "Stop"
$resolvedSDK = (Resolve-Path -LiteralPath $UE4SSRoot).Path
$sdkCMake = Join-Path $resolvedSDK "CMakeLists.txt"
if (-not (Test-Path -LiteralPath $sdkCMake -PathType Leaf)) {
  throw "UE4SSRoot must point to a complete RE-UE4SS v3.0.1 source checkout"
}
$thirdPartyCMake = Join-Path $resolvedSDK "deps\third\CMakeLists.txt"
if (-not (Test-Path -LiteralPath $thirdPartyCMake -PathType Leaf)) {
  throw "UE4SSRoot is missing deps/third/CMakeLists.txt"
}

$resolvedOutput = [System.IO.Path]::GetFullPath($OutputDir)
$bridgeSource = $PSScriptRoot.Replace("\", "/")
$bridgeBuild = (Join-Path $resolvedOutput "palpanel-bridge").Replace("\", "/")
$utf8 = [System.Text.UTF8Encoding]::new($false)
$originalSDKCMake = [System.IO.File]::ReadAllText($sdkCMake, $utf8)
$originalThirdPartyCMake = [System.IO.File]::ReadAllText($thirdPartyCMake, $utf8)
$injectedSDKCMake = $originalSDKCMake.TrimEnd() +
  "`n`nadd_subdirectory(`"$bridgeSource`" `"$bridgeBuild`")`n"
$oldCorrosionCommit = "123be1e3d8170c86e121392e8bffa4def7dc3447"
$fixedCorrosionCommit = "fce4fe54328ada11b823f9ae72346b4e97a27844"
if (-not $originalThirdPartyCMake.Contains($oldCorrosionCommit)) {
  throw "Unexpected UE4SS Corrosion pin; refusing to patch an unknown SDK revision"
}
$patchedThirdPartyCMake = $originalThirdPartyCMake.Replace(
  $oldCorrosionCommit,
  $fixedCorrosionCommit
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
  [System.IO.File]::WriteAllText($thirdPartyCMake, $patchedThirdPartyCMake, $utf8)
  cmake @cmakeArgs
  if ($LASTEXITCODE -ne 0) { throw "PalPanelBridge CMake configure failed" }

  cmake --build $resolvedOutput --target PalPanelBridge
  if ($LASTEXITCODE -ne 0) { throw "PalPanelBridge build failed" }
} finally {
  [System.IO.File]::WriteAllText($sdkCMake, $originalSDKCMake, $utf8)
  [System.IO.File]::WriteAllText($thirdPartyCMake, $originalThirdPartyCMake, $utf8)
}

$artifact = Join-Path $resolvedOutput "artifact\PalPanelBridge"
Copy-Item -Force (Join-Path $PSScriptRoot "config.ini.example") (Join-Path $artifact "config.ini.example")
Copy-Item -Force (Join-Path $PSScriptRoot "README.md") (Join-Path $artifact "README.md")
Write-Host "PalPanelBridge artifact: $artifact"
