[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ToolkitRoot,

    [Parameter(Mandatory = $true)]
    [string]$CorePath,

    [Parameter(Mandatory = $true)]
    [string]$CollaborationPath,

    [Parameter(Mandatory = $true)]
    [string]$BridgePath,

    [string]$BusinessSkill = 'skill',

    [switch]$AllowDirty
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Invoke-JSON {
    param([string]$Path, [string[]]$CliArgs)
    if ([System.IO.Path]::GetExtension($Path) -eq '.ps1') {
        $output = @(& powershell.exe -NoProfile -ExecutionPolicy Bypass -File $Path @CliArgs 2>&1)
    }
    else {
        $output = @(& $Path @CliArgs 2>&1)
    }
    if ($LASTEXITCODE -ne 0) { throw "$Path failed: $($output -join [Environment]::NewLine)" }
    return ($output -join [Environment]::NewLine) | ConvertFrom-Json
}

function Assert([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}

$toolkit = [System.IO.Path]::GetFullPath((Resolve-Path -LiteralPath $ToolkitRoot).Path)
$core = [System.IO.Path]::GetFullPath((Resolve-Path -LiteralPath $CorePath).Path)
$collaboration = [System.IO.Path]::GetFullPath((Resolve-Path -LiteralPath $CollaborationPath).Path)
$bridge = [System.IO.Path]::GetFullPath((Resolve-Path -LiteralPath $BridgePath).Path)
$consumer = Join-Path ([System.IO.Path]::GetTempPath()) ('awt-business-skill-' + [guid]::NewGuid().ToString('N'))
$coordination = Join-Path ([System.IO.Path]::GetTempPath()) ('awt-business-skill-coord-' + [guid]::NewGuid().ToString('N'))
$state = Join-Path ([System.IO.Path]::GetTempPath()) ('awt-business-skill-state-' + [guid]::NewGuid().ToString('N'))
$fixture = Join-Path $toolkit 'tests\orchestration\fixtures\role-first'
$installer = Join-Path $toolkit 'tools\orchestration\install_role_first_skill.ps1'
$entrySource = Join-Path $toolkit 'skills\multi-agent-orchestration\invoke_business_skill.ps1'
$skill = 'skill'
$requestedSkill = $BusinessSkill

New-Item -ItemType Directory -Force -Path $consumer | Out-Null
try {
    $zero = Invoke-JSON -Path $entrySource -CliArgs @('-ConsumerRoot', $consumer, '-BusinessSkill', $requestedSkill, '-Workspace', $consumer)
    Assert ($zero.entry_status -eq 'zero_install' -and $zero.directive -eq 'execute_business_skill' -and $zero.business_skill_body_preserved) 'zero-install contract failed'

    $installArguments = @(
        '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $installer,
        '-ToolkitRoot', $toolkit, '-ConsumerRoot', $consumer,
        '-SkillSource', (Join-Path $fixture 'skill'), '-RolePackage', (Join-Path $fixture 'roles'),
        '-CorePath', $core, '-CollaborationPath', $collaboration, '-BridgePath', $bridge
    )
    if ($AllowDirty) { $installArguments += '-AllowDirty' }
    & powershell.exe @installArguments | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Role-first installation failed' }
    $entry = Join-Path $consumer '.agents\skills\multi-agent-orchestration\invoke_business_skill.ps1'
    if ($requestedSkill -ne $skill) {
        $registryPath = Join-Path $consumer '.agents\agent-workflow-role-first-entry.json'
        $registry = Get-Content -LiteralPath $registryPath -Raw | ConvertFrom-Json
        $value = $registry.business_skills.PSObject.Properties[$skill].Value
        $registry.business_skills.PSObject.Properties.Remove($skill)
        $registry.business_skills | Add-Member -NotePropertyName $requestedSkill -NotePropertyValue $value
        [System.IO.File]::WriteAllText($registryPath, (($registry | ConvertTo-Json -Depth 8) + [Environment]::NewLine), [System.Text.UTF8Encoding]::new($false))
    }

    $direct = Invoke-JSON -Path $entry -CliArgs @('-ConsumerRoot', $consumer, '-BusinessSkill', $requestedSkill, '-Workspace', $consumer, '-CoordinationDir', $coordination)
    Assert ($direct.effective_mode -eq 'direct' -and $direct.directive -eq 'ordinary_delegation' -and -not $direct.gateway_allowed) 'default direct contract failed'
    Assert ([string]::Equals([string]$direct.collaboration_path, $collaboration, [System.StringComparison]::OrdinalIgnoreCase)) 'receipt-pinned collaboration path was not used'
    Assert ([string]::Equals([string]$direct.bridge_path, $bridge, [System.StringComparison]::OrdinalIgnoreCase)) 'receipt-pinned bridge path was not exposed'

    Invoke-JSON -Path $core -CliArgs @('--set-workspace-mode', '--workspace', $consumer, '--workspace-mode', 'single', '--coordination-dir', $coordination) | Out-Null
    $single = Invoke-JSON -Path $entry -CliArgs @('-ConsumerRoot', $consumer, '-BusinessSkill', $requestedSkill, '-Workspace', $consumer, '-CoordinationDir', $coordination)
    Assert ($single.effective_mode -eq 'single' -and $single.directive -eq 'execute_sequentially' -and -not $single.spawn_allowed -and -not $single.gateway_allowed) 'single contract failed'

    Invoke-JSON -Path $core -CliArgs @('--enable', '--coordination-dir', $coordination) | Out-Null
    Invoke-JSON -Path $core -CliArgs @('--set-workspace-mode', '--workspace', $consumer, '--workspace-mode', 'framework', '--coordination-dir', $coordination) | Out-Null
    $policyPath = Join-Path $consumer 'policy.json'
    $policy = Get-Content -LiteralPath (Join-Path $fixture 'native-smoke\policy.json') -Raw | ConvertFrom-Json
    $policy.registered_bindings[0].workspace = $consumer
    $policy.role_routes.'sample-workflow/reviewer'.primary.workspace = $consumer
    [System.IO.File]::WriteAllText($policyPath, (($policy | ConvertTo-Json -Depth 12) + [Environment]::NewLine), [System.Text.UTF8Encoding]::new($false))
    $framework = Invoke-JSON -Path $entry -CliArgs @('-ConsumerRoot', $consumer, '-BusinessSkill', $requestedSkill, '-Workspace', $consumer, '-Objective', 'business Skill only framework smoke', '-StateDir', $state, '-CoordinationDir', $coordination, '-PolicyFile', $policyPath)
    Assert ($framework.effective_mode -eq 'framework' -and $framework.directive -eq 'framework_controller' -and $framework.gateway_allowed) 'framework contract failed'
    Assert ($framework.route.requested_instances -eq 2 -and $framework.route.actual_instances -eq 2) 'framework distinct count failed'
    $instances = @($framework.route.run.instances)
    Assert ($instances.Count -eq 2 -and $instances[0].instance_id -ne $instances[1].instance_id) 'framework instances are not distinct'
    Assert (-not [string]::IsNullOrWhiteSpace([string]$framework.route.run.run.plan_hash)) 'frozen plan hash is missing'
    foreach ($instance in $instances) { Assert (-not [string]::IsNullOrWhiteSpace([string]$instance.role_prompt_hash)) 'Role Prompt hash is missing' }

    $disabled = Invoke-JSON -Path $core -CliArgs @('--disable', '--coordination-dir', $coordination)
    $afterDisable = Invoke-JSON -Path $entry -CliArgs @('-ConsumerRoot', $consumer, '-BusinessSkill', $requestedSkill, '-Workspace', $consumer, '-CoordinationDir', $coordination)
    Assert (-not $disabled.framework_enabled -and $afterDisable.effective_mode -eq 'direct' -and $afterDisable.route.degraded) 'disable fallback contract failed'

    $receiptPath = Join-Path $consumer '.agents\agent-workflow-toolkit.lock.json'
    $receiptRaw = Get-Content -LiteralPath $receiptPath -Raw
    $receipt = $receiptRaw | ConvertFrom-Json
    $receipt.binaries.collaboration.path = Join-Path $consumer 'missing-collaboration.exe'
    [System.IO.File]::WriteAllText($receiptPath, (($receipt | ConvertTo-Json -Depth 8) + [Environment]::NewLine), [System.Text.UTF8Encoding]::new($false))
    $failedClosed = $false
    try { & $entry -ConsumerRoot $consumer -BusinessSkill $requestedSkill -Workspace $consumer -CoordinationDir $coordination | Out-Null }
    catch { $failedClosed = $true }
    Assert $failedClosed 'missing receipt-pinned binary did not fail closed'
    [System.IO.File]::WriteAllText($receiptPath, $receiptRaw, [System.Text.UTF8Encoding]::new($false))

    [pscustomobject]@{
        status = 'passed'
        zero_install = $zero.directive
        direct = $direct.directive
        single = $single.directive
        framework = $framework.directive
        disable = $afterDisable.effective_mode
        requested_instances = $framework.route.requested_instances
        actual_instances = $framework.route.actual_instances
        pinned_collaboration_path = $direct.collaboration_path
        pinned_bridge_path = $direct.bridge_path
        missing_path_fail_closed = $failedClosed
        technical_release_contract = 'covered by public bridge/Gateway E2E after this entry creates the frozen run'
        business_approval_contract = 'technical release does not create HumanDecision; verified by Gateway E2E'
    } | ConvertTo-Json -Depth 5
}
finally {
    foreach ($path in @($consumer, $coordination, $state)) {
        if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path -Recurse -Force }
    }
}
