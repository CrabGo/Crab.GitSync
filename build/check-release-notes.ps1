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
foreach ($section in @('新增', '修改', '删除', '优化')) {
    $pattern = '(?ms)^## ' + [regex]::Escape($section) + '\r?\n(?<body>.*?)(?=^## |\z)'
    $match = [regex]::Match($notes, $pattern)
    if (-not $match.Success -or $match.Groups['body'].Value -notmatch '(?m)^- \S') {
        throw "Release notes must contain a nonempty '$section' section (use '- 无。' if not applicable)."
    }
}
Write-Output "Release notes validated: v$Version"
