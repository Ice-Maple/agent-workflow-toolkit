[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ToolkitRoot,

    [Parameter(Mandatory = $true)]
    [string]$ConsumerRoot,

    [Parameter(Mandatory = $true)]
    [string]$CorePath,

    [ValidateSet('Copy', 'SymbolicLink')]
    [string]$Mode = 'Copy',

    [switch]$Check,
    [switch]$AllowDirty
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Resolve-ExistingPath {
    param([string]$LiteralPath, [string]$Label)

    if (-not (Test-Path -LiteralPath $LiteralPath)) {
        throw "$Label does not exist: $LiteralPath"
    }
    return [System.IO.Path]::GetFullPath((Resolve-Path -LiteralPath $LiteralPath).Path)
}

function Get-TreeHash {
    param([string]$Root)

    $rootPath = [System.IO.Path]::GetFullPath($Root).TrimEnd('\', '/')
    $entries = foreach ($file in Get-ChildItem -LiteralPath $rootPath -Recurse -File | Sort-Object FullName) {
        $relative = $file.FullName.Substring($rootPath.Length).TrimStart('\', '/').Replace('\', '/')
        $hash = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        "$relative`t$hash"
    }
    $manifest = ($entries -join "`n") + "`n"
    $sha = [System.Security.Cryptography.SHA256]::Create()
    try {
        $bytes = [System.Text.UTF8Encoding]::new($false).GetBytes($manifest)
        return ([System.BitConverter]::ToString($sha.ComputeHash($bytes))).Replace('-', '').ToLowerInvariant()
    }
    finally {
        $sha.Dispose()
    }
}

function Get-GitState {
    param([string]$Root)

    $commit = (& git -C $Root rev-parse HEAD 2>&1 | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $commit -notmatch '^[0-9a-f]{40}$') {
        throw "cannot resolve toolkit commit: $commit"
    }
    $dirtyOutput = @(& git -C $Root status --porcelain=v1 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "cannot inspect toolkit worktree: $($dirtyOutput -join [Environment]::NewLine)"
    }
    return [pscustomobject]@{
        Commit = $commit
        Dirty = @($dirtyOutput | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }).Count -gt 0
    }
}

function Get-CoreVersion {
    param([string]$Path)

    $output = @(& $Path --version 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "cannot execute core --version: $($output -join [Environment]::NewLine)"
    }
    $text = ($output -join ' ').Trim()
    if ($text -notmatch '^agent-workflow-core version=(\S+) protocol=(\d+) maturity=(\S+) revision=([0-9a-fA-F]{40}|unknown) dirty=(true|false)$') {
        throw "unsupported core --version output: $text"
    }
    return [pscustomobject]@{
        Text = $text
        Version = $Matches[1]
        Protocol = [int]$Matches[2]
        Maturity = $Matches[3]
        Revision = $Matches[4].ToLowerInvariant()
        Dirty = $Matches[5] -eq 'true'
    }
}

function Assert-Equal {
    param([string]$Actual, [string]$Expected, [string]$Label)

    if (-not [string]::Equals($Actual, $Expected, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "$Label mismatch: actual=$Actual expected=$Expected"
    }
}

$toolkit = Resolve-ExistingPath $ToolkitRoot 'ToolkitRoot'
$consumer = Resolve-ExistingPath $ConsumerRoot 'ConsumerRoot'
$core = Resolve-ExistingPath $CorePath 'CorePath'
$skillSource = Resolve-ExistingPath (Join-Path $toolkit 'skills\multi-agent-orchestration') 'Controller Skill source'
if (-not (Test-Path -LiteralPath (Join-Path $skillSource 'SKILL.md') -PathType Leaf)) {
    throw "Controller Skill source is missing SKILL.md: $skillSource"
}

$gitState = Get-GitState $toolkit
if ($gitState.Dirty -and -not $AllowDirty) {
    throw 'Toolkit worktree is dirty; install from a committed checkout or pass -AllowDirty only for local development smoke'
}

$agentsRoot = Join-Path $consumer '.agents'
$skillsRoot = Join-Path $agentsRoot 'skills'
$skillTarget = Join-Path $skillsRoot 'multi-agent-orchestration'
$lockPath = Join-Path $agentsRoot 'agent-workflow-toolkit.lock.json'
$sourceHash = Get-TreeHash $skillSource
$coreHash = (Get-FileHash -LiteralPath $core -Algorithm SHA256).Hash.ToLowerInvariant()
$coreVersion = Get-CoreVersion $core
if ($coreVersion.Revision -eq 'unknown') {
    throw 'core build does not expose a VCS revision; rebuild it from the pinned toolkit checkout with Go VCS metadata enabled'
}
Assert-Equal $coreVersion.Revision $gitState.Commit 'core revision'
if ($coreVersion.Dirty -ne $gitState.Dirty) {
    throw "core/toolkit dirty state mismatch: core=$($coreVersion.Dirty) toolkit=$($gitState.Dirty); rebuild the core from the current checkout"
}

if ($Check) {
    if (-not (Test-Path -LiteralPath $lockPath -PathType Leaf)) {
        throw "installation receipt is missing: $lockPath"
    }
    if (-not (Test-Path -LiteralPath $skillTarget -PathType Container)) {
        throw "installed Controller Skill is missing: $skillTarget"
    }
    $receipt = Get-Content -LiteralPath $lockPath -Raw | ConvertFrom-Json
    if ($receipt.schema_version -ne 1) {
        throw "unsupported installation receipt schema: $($receipt.schema_version)"
    }
    Assert-Equal ([string]$receipt.toolkit_root) $toolkit 'toolkit_root'
    Assert-Equal ([string]$receipt.toolkit_commit) $gitState.Commit 'toolkit_commit'
    Assert-Equal ([string]$receipt.core_path) $core 'core_path'
    Assert-Equal ([string]$receipt.core_sha256) $coreHash 'core_sha256'
    Assert-Equal ([string]$receipt.core_version_output) $coreVersion.Text 'core_version_output'
    Assert-Equal ([string]$receipt.core_revision) $coreVersion.Revision 'core_revision'
    if ([int]$receipt.core_protocol -ne $coreVersion.Protocol) {
        throw "core_protocol mismatch: actual=$($coreVersion.Protocol) expected=$($receipt.core_protocol)"
    }
    Assert-Equal ([string]$receipt.core_maturity) $coreVersion.Maturity 'core_maturity'
    if ([bool]$receipt.core_dirty -ne $coreVersion.Dirty) {
        throw "core_dirty mismatch: actual=$($coreVersion.Dirty) expected=$($receipt.core_dirty)"
    }
    Assert-Equal ([string]$receipt.skill_source) $skillSource 'skill_source'
    Assert-Equal ([string]$receipt.skill_source_sha256) $sourceHash 'skill_source_sha256'
    Assert-Equal ([string]$receipt.skill_target) ([System.IO.Path]::GetFullPath($skillTarget)) 'skill_target'
    $installedHash = Get-TreeHash $skillTarget
    Assert-Equal $installedHash $sourceHash 'installed skill hash'
    [pscustomobject]@{
        status = 'valid'
        toolkit_commit = $gitState.Commit
        core_sha256 = $coreHash
        core_revision = $coreVersion.Revision
        skill_sha256 = $sourceHash
        skill_target = $skillTarget
    } | ConvertTo-Json -Depth 4
    exit 0
}

if (Test-Path -LiteralPath $skillTarget) {
    throw "Controller Skill target already exists; run with -Check or remove the exact target before reinstalling: $skillTarget"
}
if (Test-Path -LiteralPath $lockPath) {
    throw "installation receipt already exists; run with -Check or remove it before reinstalling: $lockPath"
}

New-Item -ItemType Directory -Force -Path $skillsRoot | Out-Null
if ($Mode -eq 'SymbolicLink') {
    New-Item -ItemType SymbolicLink -Path $skillTarget -Target $skillSource | Out-Null
}
else {
    Copy-Item -LiteralPath $skillSource -Destination $skillTarget -Recurse
}

$installedHash = Get-TreeHash $skillTarget
Assert-Equal $installedHash $sourceHash 'installed skill hash'
$receipt = [ordered]@{
    schema_version = 1
    toolkit_root = $toolkit
    toolkit_commit = $gitState.Commit
    toolkit_dirty = [bool]$gitState.Dirty
    core_path = $core
    core_sha256 = $coreHash
    core_version_output = $coreVersion.Text
    core_revision = $coreVersion.Revision
    core_dirty = [bool]$coreVersion.Dirty
    core_protocol = $coreVersion.Protocol
    core_maturity = $coreVersion.Maturity
    skill_source = $skillSource
    skill_source_sha256 = $sourceHash
    skill_target = [System.IO.Path]::GetFullPath($skillTarget)
    install_mode = $Mode
    installed_at = [DateTime]::UtcNow.ToString('o')
}
$json = $receipt | ConvertTo-Json -Depth 6
$temporaryLock = $lockPath + '.tmp-' + [guid]::NewGuid().ToString('N')
try {
    [System.IO.File]::WriteAllText($temporaryLock, $json + [Environment]::NewLine, [System.Text.UTF8Encoding]::new($false))
    Move-Item -LiteralPath $temporaryLock -Destination $lockPath
}
finally {
    if (Test-Path -LiteralPath $temporaryLock) {
        Remove-Item -LiteralPath $temporaryLock -Force
    }
}

[pscustomobject]@{
    status = 'installed'
    toolkit_commit = $gitState.Commit
    core_sha256 = $coreHash
    core_revision = $coreVersion.Revision
    skill_sha256 = $sourceHash
    skill_target = $skillTarget
    receipt = $lockPath
} | ConvertTo-Json -Depth 4
