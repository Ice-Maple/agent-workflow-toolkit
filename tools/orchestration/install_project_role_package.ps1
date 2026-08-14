[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ToolkitRoot,

    [Parameter(Mandatory = $true)]
    [string]$ConsumerRoot,

    [Parameter(Mandatory = $true)]
    [string]$RolePackage,

    [Parameter(Mandatory = $true)]
    [string]$CorePath,

    [Parameter(Mandatory = $true)]
    [string]$CollaborationPath,

    [Parameter(Mandatory = $true)]
    [string]$BridgePath,

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
$roles = Resolve-ExistingPath $RolePackage 'RolePackage'
$core = Resolve-ExistingPath $CorePath 'CorePath'
$collaborationBinary = Resolve-ExistingPath $CollaborationPath 'CollaborationPath'
$bridge = Resolve-ExistingPath $BridgePath 'BridgePath'
$rolesManifest = Join-Path $roles 'roles.yaml'
if (-not (Test-Path -LiteralPath $rolesManifest -PathType Leaf)) { throw 'RolePackage is missing roles.yaml' }

$registryOutput = @(& $core --inspect-roles --roles-manifest $rolesManifest 2>&1)
if ($LASTEXITCODE -ne 0) { throw "role registry validation failed: $($registryOutput -join [Environment]::NewLine)" }
$registryData = ($registryOutput -join [Environment]::NewLine) | ConvertFrom-Json
$rolePackageName = [string]$registryData.Package
if ([string]::IsNullOrWhiteSpace($rolePackageName) -or $rolePackageName -match '[/\\]') { throw 'RolePackage has an invalid package name' }
$roleIDs = @($registryData.Definitions.PSObject.Properties.Name | Sort-Object)
if ($roleIDs.Count -eq 0) { throw 'RolePackage does not expose any roles' }

$controllerSource = Join-Path $toolkit 'skills\multi-agent-orchestration'
$controllerTarget = Join-Path $consumer '.agents\skills\multi-agent-orchestration'
$controllerReceipt = Join-Path $consumer '.agents\agent-workflow-toolkit.lock.json'
$controllerInstaller = Join-Path $toolkit 'tools\orchestration\install_controller_skill.ps1'
$rolesTarget = Join-Path $consumer ".agents\roles\$rolePackageName"
$receiptPath = Join-Path $consumer ".agents\agent-workflow-project-role-$rolePackageName.lock.json"
$entryRegistryPath = Join-Path $consumer '.agents\agent-workflow-project-role-entry.json'
$rolesHash = Get-TreeHash $roles

$controllerArgs = @(
    '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $controllerInstaller,
    '-ToolkitRoot', $toolkit, '-ConsumerRoot', $consumer,
    '-CorePath', $core, '-CollaborationPath', $collaborationBinary, '-BridgePath', $bridge,
    '-Mode', 'Copy'
)
if ($AllowDirty) { $controllerArgs += '-AllowDirty' }
if ($Check -or (Test-Path -LiteralPath $controllerTarget) -or (Test-Path -LiteralPath $controllerReceipt)) { $controllerArgs += '-Check' }
& powershell.exe @controllerArgs | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'canonical Controller installation or receipt validation failed' }

if ($Check) {
    if (-not (Test-Path -LiteralPath $receiptPath -PathType Leaf)) { throw "project Role receipt is missing: $receiptPath" }
    if (-not (Test-Path -LiteralPath $entryRegistryPath -PathType Leaf)) { throw "project Role entry registry is missing: $entryRegistryPath" }
    $receipt = Get-Content -LiteralPath $receiptPath -Raw | ConvertFrom-Json
    if ($receipt.schema_version -ne 1) { throw "unsupported project Role receipt schema: $($receipt.schema_version)" }
    Assert-Equal ([string]$receipt.toolkit_root) $toolkit 'toolkit_root'
    Assert-Equal ([string]$receipt.controller_receipt) ([System.IO.Path]::GetFullPath($controllerReceipt)) 'controller_receipt'
    Assert-Equal ([string]$receipt.role_package_source) $roles 'role_package_source'
    Assert-Equal ([string]$receipt.role_package_target) ([System.IO.Path]::GetFullPath($rolesTarget)) 'role_package_target'
    Assert-Equal ([string]$receipt.role_package_sha256) $rolesHash 'role_package_sha256'
    Assert-Equal (Get-TreeHash $rolesTarget) $rolesHash 'installed Role Package hash'
    Assert-Equal (Get-TreeHash $controllerTarget) (Get-TreeHash $controllerSource) 'installed Controller Skill hash'
    $entryRegistry = Get-Content -LiteralPath $entryRegistryPath -Raw | ConvertFrom-Json
    if ($entryRegistry.schema_version -ne 1) { throw "unsupported project Role entry registry schema: $($entryRegistry.schema_version)" }
    Assert-Equal ([string]$entryRegistry.controller_receipt) ([System.IO.Path]::GetFullPath($controllerReceipt)) 'managed controller_receipt'
    foreach ($roleID in $roleIDs) {
        $entryProperty = $entryRegistry.roles.PSObject.Properties[$roleID]
        $entry = if ($null -eq $entryProperty) { $null } else { $entryProperty.Value }
        if ($null -eq $entry) { throw "managed project Role entry is missing: $roleID" }
        Assert-Equal ([string]$entry.roles_manifest) ([System.IO.Path]::GetFullPath((Join-Path $rolesTarget 'roles.yaml'))) "managed roles_manifest for $roleID"
        Assert-Equal ([string]$entry.role_receipt) ([System.IO.Path]::GetFullPath($receiptPath)) "managed role_receipt for $roleID"
    }
    [pscustomobject]@{ status = 'valid'; role_package_target = $rolesTarget; roles = $roleIDs; receipt = $receiptPath } | ConvertTo-Json -Depth 4
    exit 0
}

if (Test-Path -LiteralPath $receiptPath) { throw "project Role receipt already exists; run -Check: $receiptPath" }
$rolePackageReused = $false
if (Test-Path -LiteralPath $rolesTarget -PathType Container) {
    Assert-Equal (Get-TreeHash $rolesTarget) $rolesHash 'installed shared Role Package hash'
    $rolePackageReused = $true
}
else {
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $rolesTarget) | Out-Null
    Copy-Item -LiteralPath $roles -Destination $rolesTarget -Recurse
}

$gitCommit = (& git -C $toolkit rev-parse HEAD 2>&1 | Out-String).Trim()
if ($LASTEXITCODE -ne 0) { throw "cannot resolve toolkit commit: $gitCommit" }
$receipt = [ordered]@{
    schema_version = 1
    toolkit_root = $toolkit
    toolkit_commit = $gitCommit
    controller_receipt = [System.IO.Path]::GetFullPath($controllerReceipt)
    entry_registry = [System.IO.Path]::GetFullPath($entryRegistryPath)
    role_package_source = $roles
    role_package_version = [string]$registryData.Version
    role_package_sha256 = $rolesHash
    role_package_target = [System.IO.Path]::GetFullPath($rolesTarget)
    role_ids = $roleIDs
    role_package_reused = $rolePackageReused
    installed_at = [DateTime]::UtcNow.ToString('o')
}
[System.IO.File]::WriteAllText($receiptPath, (($receipt | ConvertTo-Json -Depth 6) + [Environment]::NewLine), [System.Text.UTF8Encoding]::new($false))

$entryRegistry = if (Test-Path -LiteralPath $entryRegistryPath -PathType Leaf) {
    Get-Content -LiteralPath $entryRegistryPath -Raw | ConvertFrom-Json
}
else {
    [pscustomobject]@{ schema_version = 1; controller_receipt = [System.IO.Path]::GetFullPath($controllerReceipt); roles = [pscustomobject]@{} }
}
if ($entryRegistry.schema_version -ne 1) { throw "unsupported project Role entry registry schema: $($entryRegistry.schema_version)" }
Assert-Equal ([string]$entryRegistry.controller_receipt) ([System.IO.Path]::GetFullPath($controllerReceipt)) 'managed controller_receipt'
foreach ($roleID in $roleIDs) {
    if ($null -ne $entryRegistry.roles.PSObject.Properties[$roleID]) { throw "managed project Role entry already exists: $roleID" }
    $entryRegistry.roles | Add-Member -NotePropertyName $roleID -NotePropertyValue ([ordered]@{
        roles_manifest = [System.IO.Path]::GetFullPath((Join-Path $rolesTarget 'roles.yaml'))
        role_receipt = [System.IO.Path]::GetFullPath($receiptPath)
    })
}
[System.IO.File]::WriteAllText($entryRegistryPath, (($entryRegistry | ConvertTo-Json -Depth 8) + [Environment]::NewLine), [System.Text.UTF8Encoding]::new($false))
[pscustomobject]@{ status = 'installed'; role_package_target = $rolesTarget; role_package_reused = $rolePackageReused; roles = $roleIDs; receipt = $receiptPath } | ConvertTo-Json -Depth 4
