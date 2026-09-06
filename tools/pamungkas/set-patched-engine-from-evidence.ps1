[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$EvidencePath,

    [Parameter(Mandatory = $true)]
    [string]$ReleaseTag,

    [string]$PropertiesPath = (Join-Path $PSScriptRoot '..\..\build.properties')
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if ($ReleaseTag -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$') {
    throw 'PAMUNGKAS PACKAGING NO-GO: release tag contains unsupported or unsafe characters.'
}

$resolvedEvidence = (Resolve-Path -LiteralPath $EvidencePath -ErrorAction Stop).Path
if (-not (Test-Path -LiteralPath $resolvedEvidence -PathType Leaf)) {
    throw "PAMUNGKAS PACKAGING NO-GO: dist evidence is not a regular file: $resolvedEvidence"
}

try {
    $evidence = Get-Content -LiteralPath $resolvedEvidence -Raw | ConvertFrom-Json -ErrorAction Stop
} catch {
    throw "PAMUNGKAS PACKAGING NO-GO: dist evidence JSON is invalid: $($_.Exception.Message)"
}

if ($evidence.status -ne 'DIST_PASS') {
    throw "PAMUNGKAS PACKAGING NO-GO: expected DIST_PASS evidence, found '$($evidence.status)'."
}

if (-not $evidence.installer) {
    throw 'PAMUNGKAS PACKAGING NO-GO: dist evidence has no installer record.'
}

$installerName = [IO.Path]::GetFileName([string]$evidence.installer.path)
if ($installerName -ne 'brave_installer.exe') {
    throw "PAMUNGKAS PACKAGING NO-GO: expected brave_installer.exe evidence, found '$installerName'."
}

$sha = ([string]$evidence.installer.sha256).Trim().ToLowerInvariant()
if ($sha -notmatch '^[0-9a-f]{64}$') {
    throw 'PAMUNGKAS PACKAGING NO-GO: installer evidence SHA-256 is missing or invalid.'
}

$bytes = 0L
if (-not [long]::TryParse([string]$evidence.installer.bytes, [ref]$bytes) -or $bytes -le 0) {
    throw 'PAMUNGKAS PACKAGING NO-GO: installer evidence byte size is missing or invalid.'
}

$artifactUrl = "https://github.com/pribadimartabat2/brave-core/releases/download/$ReleaseTag/brave_installer.exe"
$setter = Join-Path $PSScriptRoot 'set-patched-engine.ps1'
if (-not (Test-Path -LiteralPath $setter -PathType Leaf)) {
    throw "PAMUNGKAS PACKAGING NO-GO: governed setter is missing: $setter"
}

# PowerShell script invocation propagates terminating errors directly. Do not
# inspect $LASTEXITCODE here; that variable is for native process exit codes.
& $setter -ArtifactUrl $artifactUrl -Sha256 $sha -PropertiesPath $PropertiesPath

Write-Host 'PAMUNGKAS dist-evidence bridge PASS.'
Write-Host "Evidence: $resolvedEvidence"
Write-Host "Release tag: $ReleaseTag"
Write-Host "Artifact: $artifactUrl"
Write-Host "SHA-256: $sha"
