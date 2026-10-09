param(
    [ValidatePattern('^\d+\.\d+\.\d+$')]
    [string]$Version = '0.5.0'
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
Push-Location $projectRoot
try {
    & (Join-Path $PSScriptRoot 'check-release-notes.ps1') -Version $Version
    # Keep Windows file metadata and the updater's runtime version in agreement.
    $infoPath = Join-Path $PSScriptRoot 'windows/info.json'
    $info = Get-Content -LiteralPath $infoPath -Raw | ConvertFrom-Json
    $info.fixed.file_version = $Version
    $info.info.'0000'.ProductVersion = $Version
    $infoJson = ($info | ConvertTo-Json -Depth 10).Replace("`r`n", "`n") + "`n"
    [System.IO.File]::WriteAllText($infoPath, $infoJson, [System.Text.UTF8Encoding]::new($false))

    Push-Location frontend
    try {
        npm ci
        if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
        # Create embedded assets before Go generation and test commands.
        npm run build
        if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed' }
    } finally { Pop-Location }

    wails3 generate bindings -ts -i
    if ($LASTEXITCODE -ne 0) { throw 'Binding generation failed' }
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go vet failed' }
    wails3 build "APP_VERSION=$Version"
    if ($LASTEXITCODE -ne 0) { throw 'Desktop build failed' }

    $releaseDirectory = Join-Path $projectRoot 'bin/release'
    New-Item -ItemType Directory -Path $releaseDirectory -Force | Out-Null
    $artifact = Join-Path $releaseDirectory 'crab-gitsync-windows-amd64.exe'
    Copy-Item -LiteralPath 'bin/crab-gitsync.exe' -Destination $artifact -Force
    $digest = (Get-FileHash -LiteralPath $artifact -Algorithm SHA256).Hash.ToLowerInvariant()
    "$digest  crab-gitsync-windows-amd64.exe" | Set-Content -LiteralPath (Join-Path $releaseDirectory 'SHA256SUMS') -Encoding ascii
    Write-Output "Release v$Version prepared in $releaseDirectory"
} finally { Pop-Location }
