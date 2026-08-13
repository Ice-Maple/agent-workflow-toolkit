[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ToolkitRoot,

    [Parameter(Mandatory = $true)]
    [string]$ConsumerRoot,

    [Parameter(Mandatory = $true)]
    [string]$SkillSource,

    [Parameter(Mandatory = $true)]
    [string]$RolePackage,

    [Parameter(Mandatory = $true)]
    [string]$CorePath,

    [switch]$Check,
    [switch]$AllowDirty
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Resolve-ExistingPath([string]$Path, [string]$Label) {
    if (-not (Test-Path -LiteralPath $Path)) { throw "$Label does not exist: $Path" }
    return [System.IO.Path]::GetFullPath((Resolve-Path -LiteralPath $Path).Path)
}

function Get-TreeHash([string]$Root) {
    $rootPath = [System.IO.Path]::GetFullPath($Root).TrimEnd('\', '/')
    $entries = foreach ($file in Get-ChildItem -LiteralPath $rootPath -Recurse -File | Sort-Object FullName) {
        $relative = $file.FullName.Substring($rootPath.Length).TrimStart('\', '/').Replace('\', '/')
        $hash = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        "$relative`t$hash"
    }
    $sha = [System.Security.Cryptography.SHA256]::Create()
    try {
        $bytes = [System.Text.UTF8Encoding]::new($false).GetBytes((($entries -join "`n") + "`n"))
        return ([System.BitConverter]::ToString($sha.ComputeHash($bytes))).Replace('-', '').ToLowerInvariant()
    }
    finally { $sha.Dispose() }
}

function Assert-Equal([string]$Actual, [string]$Expected, [string]$Label) {
    if (-not [string]::Equals($Actual, $Expected, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "$Label mismatch: actual=$Actual expected=$Expected"
    }
}

$toolkit = Resolve-ExistingPath $ToolkitRoot 'ToolkitRoot'
$consumer = Resolve-ExistingPath $ConsumerRoot 'ConsumerRoot'
$skill = Resolve-ExistingPath $SkillSource 'SkillSource'
$roles = Resolve-ExistingPath $RolePackage 'RolePackage'
$core = Resolve-ExistingPath $CorePath 'CorePath'
$sidecar = Join-Path $skill 'collaboration.yaml'
$rolesManifest = Join-Path $roles 'roles.yaml'
if (-not (Test-Path -LiteralPath (Join-Path $skill 'SKILL.md') -PathType Leaf)) { throw 'SkillSource is missing SKILL.md' }
if (-not (Test-Path -LiteralPath $sidecar -PathType Leaf)) { throw 'SkillSource is missing collaboration.yaml' }
if (-not (Test-Path -LiteralPath $rolesManifest -PathType Leaf)) { throw 'RolePackage is missing roles.yaml' }

$collaboration = (& $core --inspect-collaboration --collaboration-file $sidecar 2>&1 | Out-String)
if ($LASTEXITCODE -ne 0) { throw "collaboration validation failed: $collaboration" }
$registry = (& $core --inspect-roles --roles-manifest $rolesManifest 2>&1 | Out-String)
if ($LASTEXITCODE -ne 0) { throw "role registry validation failed: $registry" }
$collaborationData = $collaboration | ConvertFrom-Json
$registryData = $registry | ConvertFrom-Json
$registeredRoles = @($registryData.Definitions.PSObject.Properties.Name)
foreach ($dependency in @($collaborationData.role_dependencies)) {
    if ($registeredRoles -notcontains [string]$dependency) { throw "role dependency is missing from RolePackage: $dependency" }
}

$skillName = Split-Path -Leaf $skill
$rolePackageName = [string]$registryData.Package
if ([string]::IsNullOrWhiteSpace($rolePackageName) -or $rolePackageName -match '[/\\]') { throw 'RolePackage has an invalid package name' }
$skillTarget = Join-Path $consumer ".agents\skills\$skillName"
$rolesTarget = Join-Path $consumer ".agents\roles\$rolePackageName"
$controllerSource = Join-Path $toolkit 'skills\multi-agent-orchestration'
$controllerTarget = Join-Path $consumer '.agents\skills\multi-agent-orchestration'
$controllerReceipt = Join-Path $consumer '.agents\agent-workflow-toolkit.lock.json'
$controllerInstaller = Join-Path $toolkit 'tools\orchestration\install_controller_skill.ps1'
$receiptPath = Join-Path $consumer ".agents\agent-workflow-role-first-$skillName.lock.json"
$skillHash = Get-TreeHash $skill
$rolesHash = Get-TreeHash $roles

if ($Check) {
    if (-not (Test-Path -LiteralPath $receiptPath -PathType Leaf)) { throw "receipt is missing: $receiptPath" }
    $receipt = Get-Content -LiteralPath $receiptPath -Raw | ConvertFrom-Json
    Assert-Equal ([string]$receipt.toolkit_root) $toolkit 'toolkit_root'
    Assert-Equal ([string]$receipt.skill_source_sha256) $skillHash 'skill_source_sha256'
    Assert-Equal ([string]$receipt.role_package_sha256) $rolesHash 'role_package_sha256'
    Assert-Equal (Get-TreeHash $skillTarget) $skillHash 'installed skill hash'
    Assert-Equal (Get-TreeHash $rolesTarget) $rolesHash 'installed Role Package hash'
    Assert-Equal (Get-TreeHash $controllerTarget) (Get-TreeHash $controllerSource) 'installed Controller Skill hash'
    $controllerArgs = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $controllerInstaller, '-ToolkitRoot', $toolkit, '-ConsumerRoot', $consumer, '-CorePath', $core, '-Mode', 'Copy', '-Check')
    if ($AllowDirty) { $controllerArgs += '-AllowDirty' }
    & powershell.exe @controllerArgs | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'canonical Controller installation receipt validation failed' }
    [pscustomobject]@{ status = 'valid'; skill_target = $skillTarget; role_package_target = $rolesTarget; receipt = $receiptPath } | ConvertTo-Json
    exit 0
}

foreach ($target in @($skillTarget, $receiptPath)) {
    if (Test-Path -LiteralPath $target) { throw "target already exists; run -Check or remove the exact target before reinstalling: $target" }
}
$controllerArgs = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $controllerInstaller, '-ToolkitRoot', $toolkit, '-ConsumerRoot', $consumer, '-CorePath', $core, '-Mode', 'Copy')
if ($AllowDirty) { $controllerArgs += '-AllowDirty' }
if ((Test-Path -LiteralPath $controllerTarget) -or (Test-Path -LiteralPath $controllerReceipt)) {
    $controllerArgs += '-Check'
}
& powershell.exe @controllerArgs | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'canonical Controller Skill installation failed' }
$controllerHash = Get-TreeHash $controllerSource
$rolePackageReused = $false
if (Test-Path -LiteralPath $rolesTarget -PathType Container) {
    $installedRolesHash = Get-TreeHash $rolesTarget
    Assert-Equal $installedRolesHash $rolesHash 'installed shared Role Package hash'
    $rolePackageReused = $true
}
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $skillTarget), (Split-Path -Parent $rolesTarget) | Out-Null
Copy-Item -LiteralPath $skill -Destination $skillTarget -Recurse
if (-not $rolePackageReused) { Copy-Item -LiteralPath $roles -Destination $rolesTarget -Recurse }

$gitCommit = (& git -C $toolkit rev-parse HEAD 2>&1 | Out-String).Trim()
if ($LASTEXITCODE -ne 0) { throw "cannot resolve toolkit commit: $gitCommit" }
$receipt = [ordered]@{
    schema_version = 1
    toolkit_root = $toolkit
    toolkit_commit = $gitCommit
    core_path = $core
    skill_source = $skill
    skill_source_sha256 = $skillHash
    skill_target = $skillTarget
    controller_skill_sha256 = $controllerHash
    controller_skill_target = $controllerTarget
    role_package_source = $roles
    role_package_version = [string]$registryData.Version
    role_package_sha256 = $rolesHash
    role_package_target = $rolesTarget
    role_dependencies = @($collaborationData.role_dependencies)
    role_package_reused = $rolePackageReused
    installed_at = [DateTime]::UtcNow.ToString('o')
}
[System.IO.File]::WriteAllText($receiptPath, (($receipt | ConvertTo-Json -Depth 6) + [Environment]::NewLine), [System.Text.UTF8Encoding]::new($false))
[pscustomobject]@{ status = 'installed'; skill_target = $skillTarget; role_package_target = $rolesTarget; role_package_reused = $rolePackageReused; receipt = $receiptPath } | ConvertTo-Json
