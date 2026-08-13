[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ConsumerRoot,

    [Parameter(Mandatory = $true)]
    [string]$BusinessSkill,

    [Parameter(Mandatory = $true)]
    [string]$Workspace,

    [string]$Objective = '',
    [string]$Title = '',
    [string]$StateDir = '',
    [string]$CoordinationDir = '',
    [string]$PolicyFile = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Resolve-CanonicalPath([string]$Path, [string]$Label, [string]$PathType = 'Any') {
    $arguments = @{ LiteralPath = $Path }
    if ($PathType -ne 'Any') { $arguments.PathType = $PathType }
    if (-not (Test-Path @arguments)) { throw "$Label does not exist: $Path" }
    return [System.IO.Path]::GetFullPath((Resolve-Path -LiteralPath $Path).Path)
}

function Write-EntryResult([hashtable]$Values) {
    [pscustomobject]$Values | ConvertTo-Json -Depth 30
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

$consumer = Resolve-CanonicalPath $ConsumerRoot 'ConsumerRoot' 'Container'
$workspacePath = Resolve-CanonicalPath $Workspace 'Workspace' 'Container'
$registryPath = Join-Path $consumer '.agents\agent-workflow-role-first-entry.json'
if (-not (Test-Path -LiteralPath $registryPath -PathType Leaf)) {
    Write-EntryResult ([ordered]@{
        schema_version = 1
        entry_status = 'zero_install'
        effective_mode = 'direct'
        directive = 'execute_business_skill'
        business_skill = $BusinessSkill
        business_skill_body_preserved = $true
        use_controller = $false
        spawn_allowed = $null
        gateway_allowed = $false
        reason = 'Role-first Router is not installed; execute the original business Skill body'
    })
    exit 0
}

$registry = Get-Content -LiteralPath $registryPath -Raw | ConvertFrom-Json
if ($registry.schema_version -ne 1) { throw "unsupported managed entry registry schema: $($registry.schema_version)" }
$entryProperty = $registry.business_skills.PSObject.Properties[$BusinessSkill]
$entry = if ($null -eq $entryProperty) { $null } else { $entryProperty.Value }
if ($null -eq $entry) { throw "business Skill is not registered for Role-first entry: $BusinessSkill" }
$controllerReceiptPath = Resolve-CanonicalPath ([string]$registry.controller_receipt) 'Controller receipt' 'Leaf'
$roleReceiptPath = Resolve-CanonicalPath ([string]$entry.role_receipt) 'Role-first receipt' 'Leaf'
$skillPath = Resolve-CanonicalPath ([string]$entry.skill_path) 'Business Skill' 'Container'
$collaborationFile = Resolve-CanonicalPath ([string]$entry.collaboration_file) 'collaboration.yaml' 'Leaf'
$rolesManifest = Resolve-CanonicalPath ([string]$entry.roles_manifest) 'roles.yaml' 'Leaf'
$controllerReceipt = Get-Content -LiteralPath $controllerReceiptPath -Raw | ConvertFrom-Json
$roleReceipt = Get-Content -LiteralPath $roleReceiptPath -Raw | ConvertFrom-Json
if ($controllerReceipt.schema_version -ne 2) { throw "unsupported Controller receipt schema: $($controllerReceipt.schema_version)" }
if ($roleReceipt.schema_version -ne 2) { throw "unsupported Role-first receipt schema: $($roleReceipt.schema_version)" }
if (-not [string]::Equals([string]$roleReceipt.controller_receipt, $controllerReceiptPath, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw 'Role-first receipt does not bind the selected Controller receipt'
}
Assert-Equal ([string]$roleReceipt.toolkit_root) ([string]$controllerReceipt.toolkit_root) 'Role-first toolkit_root'
Assert-Equal ([string]$roleReceipt.toolkit_commit) ([string]$controllerReceipt.toolkit_commit) 'Role-first toolkit_commit'
Assert-Equal ([string]$roleReceipt.skill_target) $skillPath 'Role-first skill_target'
Assert-Equal ([string]$roleReceipt.role_package_target) ([System.IO.Path]::GetDirectoryName($rolesManifest)) 'Role-first role_package_target'
Assert-Equal ([string]$entry.skill_path) $skillPath 'managed skill_path'
Assert-Equal ([string]$entry.collaboration_file) (Join-Path $skillPath 'collaboration.yaml') 'managed collaboration_file'
Assert-Equal ([string]$entry.roles_manifest) (Join-Path ([string]$roleReceipt.role_package_target) 'roles.yaml') 'managed roles_manifest'
Assert-Equal (Get-TreeHash $skillPath) ([string]$roleReceipt.skill_source_sha256) 'installed business Skill hash'
Assert-Equal (Get-TreeHash ([string]$roleReceipt.role_package_target)) ([string]$roleReceipt.role_package_sha256) 'installed Role Package hash'
foreach ($name in @('core', 'collaboration', 'bridge')) {
    $record = $controllerReceipt.binaries.$name
    if ($null -eq $record) { throw "Controller receipt binary record is missing: $name" }
    $path = Resolve-CanonicalPath ([string]$record.path) "$name binary" 'Leaf'
    $hash = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
    if (-not [string]::Equals($hash, [string]$record.sha256, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "$name binary hash mismatch"
    }
}

$toolkit = Resolve-CanonicalPath ([string]$controllerReceipt.toolkit_root) 'ToolkitRoot' 'Container'
$installer = Resolve-CanonicalPath (Join-Path $toolkit 'tools\orchestration\install_controller_skill.ps1') 'Controller installer' 'Leaf'
$checkArguments = @(
    '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $installer,
    '-ToolkitRoot', $toolkit, '-ConsumerRoot', $consumer,
    '-CorePath', [string]$controllerReceipt.binaries.core.path,
    '-CollaborationPath', [string]$controllerReceipt.binaries.collaboration.path,
    '-BridgePath', [string]$controllerReceipt.binaries.bridge.path,
    '-Mode', 'Copy', '-Check'
)
if ([bool]$controllerReceipt.toolkit_dirty) { $checkArguments += '-AllowDirty' }
& powershell.exe @checkArguments | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Controller receipt validation failed' }

$collaborationBinary = [string]$controllerReceipt.binaries.collaboration.path
$arguments = @(
    '--collaboration-file', $collaborationFile,
    '--roles-manifest', $rolesManifest,
    '--workspace', $workspacePath,
    '--coordination-dir', $CoordinationDir
)
if (-not [string]::IsNullOrWhiteSpace($Objective)) { $arguments += @('--objective', $Objective) }
if (-not [string]::IsNullOrWhiteSpace($Title)) { $arguments += @('--title', $Title) }
if (-not [string]::IsNullOrWhiteSpace($StateDir)) { $arguments += @('--state-dir', $StateDir) }
if (-not [string]::IsNullOrWhiteSpace($PolicyFile)) { $arguments += @('--policy-file', $PolicyFile) }
$raw = @(& $collaborationBinary @arguments 2>&1)
if ($LASTEXITCODE -ne 0) {
    throw "Role-first framework entry failed after managed routing; direct fallback is forbidden: $($raw -join [Environment]::NewLine)"
}
$route = ($raw -join [Environment]::NewLine) | ConvertFrom-Json
$directive = switch ([string]$route.effective_mode) {
    'framework' { 'framework_controller' }
    'direct' { 'ordinary_delegation' }
    'single' { 'execute_sequentially' }
    default { throw "unsupported effective mode: $($route.effective_mode)" }
}
Write-EntryResult ([ordered]@{
    schema_version = 1
    entry_status = 'managed'
    effective_mode = [string]$route.effective_mode
    directive = $directive
    business_skill = $BusinessSkill
    business_skill_path = $skillPath
    business_skill_body_preserved = $true
    use_controller = [string]$route.effective_mode -eq 'framework'
    spawn_allowed = [string]$route.effective_mode -ne 'single'
    gateway_allowed = [string]$route.effective_mode -eq 'framework'
    controller_receipt = $controllerReceiptPath
    core_path = [string]$controllerReceipt.binaries.core.path
    collaboration_path = $collaborationBinary
    bridge_path = [string]$controllerReceipt.binaries.bridge.path
    route = $route
})
