param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^\d+\.\d+\.\d+$')]
    [string]$Version
)
$ErrorActionPreference = 'Stop'
$notesPath = Join-Path (Split-Path $PSScriptRoot -Parent) "releases/v$Version.md"
if (-not (Test-Path -LiteralPath $notesPath -PathType Leaf)) {
    throw "Missing release notes: $notesPath"
}
$notes = Get-Content -LiteralPath $notesPath -Raw -Encoding utf8
if (-not $notes.TrimStart([char]0xFEFF).StartsWith("# Crab.GitSync v$Version`n") -and -not $notes.TrimStart([char]0xFEFF).StartsWith("# Crab.GitSync v$Version`r`n")) {
    throw "Release notes title must match the executable version: # Crab.GitSync v$Version"
}
if ((Get-Item -LiteralPath $notesPath).Length -gt 65536) {
    throw 'Release notes exceed the in-app 64 KiB limit.'
}
foreach ($section in @('新增', '修改', '删除', '优化')) {
    $pattern = '(?ms)^## ' + [regex]::Escape($section) + '\r?\n(?<body>.*?)(?=^## |\z)'
    $match = [regex]::Match($notes, $pattern)
    if (-not $match.Success -or $match.Groups['body'].Value -notmatch '(?m)^- \S') {
        throw "Release notes must contain a nonempty '$section' section (use '- 无。' if not applicable)."
    }
}
Write-Output "Release notes validated: v$Version"
