[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ArtifactUrl,

    [Parameter(Mandatory = $true)]
    [string]$Sha256,

    [string]$PropertiesPath = (Join-Path $PSScriptRoot '..\..\build.properties')
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$allowedPrefix = 'https://github.com/pribadimartabat2/brave-core/releases/download/'
$normalizedSha = $Sha256.Trim().ToLowerInvariant()

if (-not $ArtifactUrl.StartsWith($allowedPrefix, [StringComparison]::Ordinal)) {
    throw "PAMUNGKAS PACKAGING NO-GO: artifact URL must start with $allowedPrefix"
}

if ($normalizedSha -notmatch '^[0-9a-f]{64}$') {
    throw 'PAMUNGKAS PACKAGING NO-GO: SHA-256 must be exactly 64 hexadecimal characters.'
}

$resolvedProperties = (Resolve-Path -LiteralPath $PropertiesPath -ErrorAction Stop).Path
if (-not (Test-Path -LiteralPath $resolvedProperties -PathType Leaf)) {
    throw "PAMUNGKAS PACKAGING NO-GO: build.properties is not a regular file: $resolvedProperties"
}

$original = [IO.File]::ReadAllText($resolvedProperties)
$urlPattern = '(?m)^atf\.win64\.url[ \t]*=[ \t]*.*$'
$urlMatches = [regex]::Matches($original, $urlPattern)
if ($urlMatches.Count -ne 1) {
    throw "PAMUNGKAS PACKAGING NO-GO: expected exactly one atf.win64.url entry, found $($urlMatches.Count)."
}

$updated = [regex]::Replace(
    $original,
    $urlPattern,
    "atf.win64.url = $ArtifactUrl",
    1
)

$shaPattern = '(?m)^pamungkas\.engine\.sha256[ \t]*=[ \t]*.*$'
$shaMatches = [regex]::Matches($updated, $shaPattern)
if ($shaMatches.Count -gt 1) {
    throw "PAMUNGKAS PACKAGING NO-GO: multiple pamungkas.engine.sha256 entries found."
}

$shaLine = "pamungkas.engine.sha256 = $normalizedSha"
if ($shaMatches.Count -eq 1) {
    $updated = [regex]::Replace($updated, $shaPattern, $shaLine, 1)
} else {
    $updated = $updated.TrimEnd("`r", "`n") + "`n`n# PAMUNGKAS patched engine authority`n$shaLine`n"
}

if ($updated.Contains('brave-browser-downloads.s3.brave.com')) {
    throw 'PAMUNGKAS PACKAGING NO-GO: stock Brave S3 source remains after configuration.'
}
if (-not $updated.Contains("atf.win64.url = $ArtifactUrl")) {
    throw 'PAMUNGKAS PACKAGING NO-GO: patched artifact URL verification failed before write.'
}
if (-not $updated.Contains($shaLine)) {
    throw 'PAMUNGKAS PACKAGING NO-GO: patched artifact SHA-256 verification failed before write.'
}

$directory = Split-Path -Parent $resolvedProperties
$tempPath = Join-Path $directory ('.build.properties.pamungkas-' + [guid]::NewGuid().ToString('N') + '.tmp')
try {
    [IO.File]::WriteAllText($tempPath, $updated, [Text.UTF8Encoding]::new($false))
    Move-Item -LiteralPath $tempPath -Destination $resolvedProperties -Force
} finally {
    if (Test-Path -LiteralPath $tempPath) {
        Remove-Item -LiteralPath $tempPath -Force
    }
}

Write-Host 'PAMUNGKAS patched-engine configuration PASS.'
Write-Host "Artifact: $ArtifactUrl"
Write-Host "SHA-256: $normalizedSha"
