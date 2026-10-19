# Cardo launcher installer for Windows.
#
#   irm https://raw.githubusercontent.com/amanachenko/cardo/main/install.ps1 | iex
#
# If the execution policy blocks the one-liner, use the documented form instead of changing the
# policy for the machine:
#
#   powershell -ExecutionPolicy Bypass -File .\install.ps1
#
# What it does: detect the architecture, download the zip and checksums.txt for the same release,
# verify the SHA-256, install cardo.exe under %LOCALAPPDATA%\Cardo\bin, and add that directory to the
# *user* PATH. No administrator rights, no machine-wide changes, no execution-policy change.
#
# Environment:
#   CARDO_VERSION   release tag to install (default: latest)
#   CARDO_BIN       install directory (default: %LOCALAPPDATA%\Cardo\bin)
#   CARDO_BASE_URL  override the download base (used by the offline smoke test)

$ErrorActionPreference = 'Stop'
$repo = 'amanachenko/cardo'
$base = if ($env:CARDO_BASE_URL) {
    $env:CARDO_BASE_URL
} elseif ($env:CARDO_VERSION) {
    "https://github.com/$repo/releases/download/$($env:CARDO_VERSION)"
} else {
    "https://github.com/$repo/releases/latest/download"
}
$binDir = if ($env:CARDO_BIN) { $env:CARDO_BIN } else { Join-Path $env:LOCALAPPDATA 'Cardo\bin' }

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'ARM64' { 'arm64' }
    'AMD64' { 'amd64' }
    default { throw "unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}
$asset = "cardo_windows_$arch.zip"
Write-Host "platform: windows/$arch"

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("cardo-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    $zip = Join-Path $tmp $asset
    $sums = Join-Path $tmp 'checksums.txt'
    Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile $zip
    Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile $sums

    $expected = (Get-Content $sums | Where-Object { $_ -match "\s$([regex]::Escape($asset))$" }) -split '\s+' | Select-Object -First 1
    if (-not $expected) { throw "checksums.txt has no entry for $asset" }
    $actual = (Get-FileHash -Algorithm SHA256 -Path $zip).Hash.ToLower()
    if ($actual -ne $expected.ToLower()) { throw "checksum mismatch for $asset: refusing to install" }

    Expand-Archive -Path $zip -DestinationPath $tmp -Force
    $exe = Get-ChildItem -Path $tmp -Filter 'cardo.exe' -Recurse | Select-Object -First 1
    if (-not $exe) { throw 'the archive did not contain cardo.exe' }

    New-Item -ItemType Directory -Path $binDir -Force | Out-Null
    Copy-Item $exe.FullName (Join-Path $binDir 'cardo.exe') -Force

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not ($userPath -split ';' | Where-Object { $_ -eq $binDir })) {
        $newPath = if ([string]::IsNullOrEmpty($userPath)) { $binDir } else { "$userPath;$binDir" }
        [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
        Write-Host "$binDir added to your user PATH (new terminals only)"
    }

    $version = & (Join-Path $binDir 'cardo.exe') version
    Write-Host "installed: $binDir\cardo.exe ($version)"
    Write-Host ''
    Write-Host 'next: cardo ui      (starts the installer; Docker Desktop must be running)'
}
finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
