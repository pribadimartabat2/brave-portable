$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$scriptPath = Join-Path $PSScriptRoot 'set-patched-engine-from-evidence.ps1'
$sha = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef'

function New-Fixture {
    $dir = Join-Path ([IO.Path]::GetTempPath()) ("pamungkas-evidence-test-" + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $dir | Out-Null

    $properties = Join-Path $dir 'build.properties'
    @"
app = brave
atf.win64.filename = brave_installer-win64
atf.win64.ext = .exe
atf.win64.url = https://brave-browser-downloads.s3.brave.com/latest/brave_installer-x64.exe
atf.win64.assertextract = chrome.7z
"@ | Set-Content -LiteralPath $properties -Encoding UTF8

    $evidence = Join-Path $dir 'windows-dist-result.json'
    [ordered]@{
        status = 'DIST_PASS'
        brave_core_commit = 'abcdef1234567890'
        installer = [ordered]@{
            path = 'C:\build\src\out\Release\brave_installer.exe'
            bytes = 123456
            sha256 = $sha
        }
        distribution_zips = @()
    } | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $evidence -Encoding UTF8

    return [pscustomobject]@{ Dir = $dir; Properties = $properties; Evidence = $evidence }
}

function Expect-Failure([scriptblock]$Action, [string]$Message) {
    $failed = $false
    try { & $Action } catch { $failed = $true }
    if (-not $failed) { throw $Message }
}

$fixture = New-Fixture
$before = Get-Content -LiteralPath $fixture.Properties -Raw
Expect-Failure {
    & $scriptPath -EvidencePath $fixture.Evidence -ReleaseTag '../bad' -PropertiesPath $fixture.Properties
} 'unsafe release tag must be rejected'
if ((Get-Content -LiteralPath $fixture.Properties -Raw) -ne $before) {
    throw 'unsafe tag changed build.properties'
}
Remove-Item -Recurse -Force $fixture.Dir

$fixture = New-Fixture
$bad = Get-Content -LiteralPath $fixture.Evidence -Raw | ConvertFrom-Json
$bad.status = 'BUILD_PASS'
$bad | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $fixture.Evidence -Encoding UTF8
$before = Get-Content -LiteralPath $fixture.Properties -Raw
Expect-Failure {
    & $scriptPath -EvidencePath $fixture.Evidence -ReleaseTag 'pamungkas-poc-1.97.8-test' -PropertiesPath $fixture.Properties
} 'non-DIST_PASS evidence must be rejected'
if ((Get-Content -LiteralPath $fixture.Properties -Raw) -ne $before) {
    throw 'rejected evidence changed build.properties'
}
Remove-Item -Recurse -Force $fixture.Dir

$fixture = New-Fixture
& $scriptPath -EvidencePath $fixture.Evidence -ReleaseTag 'pamungkas-poc-1.97.8-test' -PropertiesPath $fixture.Properties
$result = Get-Content -LiteralPath $fixture.Properties -Raw
$expectedUrl = 'https://github.com/pribadimartabat2/brave-core/releases/download/pamungkas-poc-1.97.8-test/brave_installer.exe'
if (-not $result.Contains("atf.win64.url = $expectedUrl")) {
    throw 'derived patched-engine URL missing'
}
if (-not $result.Contains("pamungkas.engine.sha256 = $sha")) {
    throw 'evidence SHA-256 missing'
}
Remove-Item -Recurse -Force $fixture.Dir

Write-Host 'set-patched-engine-from-evidence_test: PASS'
