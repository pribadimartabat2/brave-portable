$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$scriptPath = Join-Path $PSScriptRoot 'set-patched-engine.ps1'
$allowedUrl = 'https://github.com/pribadimartabat2/brave-core/releases/download/poc-123/brave_installer.exe'
$validSha = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef'
$stockUrl = 'https://brave-browser-downloads.s3.brave.com/latest/brave_installer-x64.exe'

function New-TestProperties {
    $dir = Join-Path ([IO.Path]::GetTempPath()) ("pamungkas-engine-test-" + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $dir | Out-Null
    $path = Join-Path $dir 'build.properties'
    @"
app = brave
atf.win64.filename = brave_installer-win64
atf.win64.ext = .exe
atf.win64.url = $stockUrl
atf.win64.assertextract = chrome.7z
"@ | Set-Content -LiteralPath $path -Encoding UTF8
    return $path
}

function Expect-Failure([scriptblock]$Action, [string]$Message) {
    $failed = $false
    try {
        & $Action
    } catch {
        $failed = $true
    }
    if (-not $failed) {
        throw $Message
    }
}

$path = New-TestProperties
$before = Get-Content -LiteralPath $path -Raw
Expect-Failure {
    & $scriptPath -PropertiesPath $path -ArtifactUrl $stockUrl -Sha256 $validSha
} 'stock Brave URL must be rejected'
if ((Get-Content -LiteralPath $path -Raw) -ne $before) {
    throw 'rejected stock URL changed build.properties'
}
Remove-Item -Recurse -Force (Split-Path $path)

$path = New-TestProperties
$before = Get-Content -LiteralPath $path -Raw
Expect-Failure {
    & $scriptPath -PropertiesPath $path -ArtifactUrl $allowedUrl -Sha256 'bad'
} 'invalid SHA-256 must be rejected'
if ((Get-Content -LiteralPath $path -Raw) -ne $before) {
    throw 'rejected SHA-256 changed build.properties'
}
Remove-Item -Recurse -Force (Split-Path $path)

$path = New-TestProperties
& $scriptPath -PropertiesPath $path -ArtifactUrl $allowedUrl -Sha256 $validSha
$result = Get-Content -LiteralPath $path -Raw
if (-not $result.Contains("atf.win64.url = $allowedUrl")) {
    throw 'governed patched engine URL was not written'
}
if (-not $result.Contains("pamungkas.engine.sha256 = $validSha")) {
    throw 'governed patched engine SHA-256 was not written'
}
if ($result.Contains('brave-browser-downloads.s3.brave.com')) {
    throw 'stock Brave URL survived governed configuration'
}
Remove-Item -Recurse -Force (Split-Path $path)

Write-Host 'set-patched-engine_test: PASS'
