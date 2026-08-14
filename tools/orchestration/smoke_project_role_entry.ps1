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
$consumer = Join-Path ([System.IO.Path]::GetTempPath()) ('awt-project-role-' + [guid]::NewGuid().ToString('N'))
$coordination = Join-Path ([System.IO.Path]::GetTempPath()) ('awt-project-role-coord-' + [guid]::NewGuid().ToString('N'))
$state = Join-Path ([System.IO.Path]::GetTempPath()) ('awt-project-role-state-' + [guid]::NewGuid().ToString('N'))
$frozenRoot = Join-Path ([System.IO.Path]::GetTempPath()) ('awt-project-role-input-' + [guid]::NewGuid().ToString('N'))
$frozenArtifact = Join-Path $frozenRoot 'git-review.md'
$fixture = Join-Path $toolkit 'tests\orchestration\fixtures\role-first'
$installer = Join-Path $toolkit 'tools\orchestration\install_project_role_package.ps1'
$entrySource = Join-Path $toolkit 'skills\multi-agent-orchestration\invoke_project_role.ps1'
$role = 'sample-workflow/reviewer'
$task = 'review the current diff'

New-Item -ItemType Directory -Force -Path $consumer | Out-Null
New-Item -ItemType Directory -Force -Path $frozenRoot | Out-Null
[System.IO.File]::WriteAllText($frozenArtifact, "# Frozen Git review input`n`n## Git status`n`n``````text`n M sample.go`n```````n", [System.Text.UTF8Encoding]::new($false))
try {
    $zero = Invoke-JSON -Path $entrySource -CliArgs @('-ConsumerRoot', $consumer, '-Role', $role, '-Task', $task, '-Workspace', $consumer, '-FrozenInputArtifact', $frozenArtifact)
    Assert ($zero.entry_status -eq 'zero_install' -and $zero.directive -eq 'ordinary_delegation' -and -not $zero.gateway_allowed) 'zero-install project Role contract failed'
    Assert ([string]::Equals([string]$zero.frozen_input.sha256, (Get-FileHash -LiteralPath $frozenArtifact -Algorithm SHA256).Hash, [System.StringComparison]::OrdinalIgnoreCase)) 'zero-install project Role did not preserve the frozen input receipt'

    $installArguments = @(
        '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $installer,
        '-ToolkitRoot', $toolkit, '-ConsumerRoot', $consumer,
        '-RolePackage', (Join-Path $fixture 'roles'),
        '-CorePath', $core, '-CollaborationPath', $collaboration, '-BridgePath', $bridge
    )
    if ($AllowDirty) { $installArguments += '-AllowDirty' }
    & powershell.exe @installArguments | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'project Role installation failed' }

    $checkArguments = @($installArguments + '-Check')
    & powershell.exe @checkArguments | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'project Role installation -Check failed' }
    $entry = Join-Path $consumer '.agents\skills\multi-agent-orchestration\invoke_project_role.ps1'

    $direct = Invoke-JSON -Path $entry -CliArgs @('-ConsumerRoot', $consumer, '-Role', $role, '-Task', $task, '-Workspace', $consumer, '-CoordinationDir', $coordination, '-FrozenInputArtifact', $frozenArtifact)
    Assert ($direct.effective_mode -eq 'direct' -and $direct.directive -eq 'ordinary_delegation' -and $direct.spawn_allowed -and -not $direct.gateway_allowed) 'default direct project Role contract failed'
    Assert ((Test-Path -LiteralPath $direct.role_prompt_path -PathType Leaf) -and -not [string]::IsNullOrWhiteSpace([string]$direct.role_prompt_hash)) 'verified project Role Prompt was not returned'
    Assert ([string]::Equals([string]$direct.collaboration_path, $collaboration, [System.StringComparison]::OrdinalIgnoreCase)) 'receipt-pinned collaboration path was not used'
    Assert ([string]::Equals([string]$direct.frozen_input.sha256, (Get-FileHash -LiteralPath $frozenArtifact -Algorithm SHA256).Hash, [System.StringComparison]::OrdinalIgnoreCase)) 'frozen input hash was not routed'

    Invoke-JSON -Path $core -CliArgs @('--set-workspace-mode', '--workspace', $consumer, '--workspace-mode', 'single', '--coordination-dir', $coordination) | Out-Null
    $single = Invoke-JSON -Path $entry -CliArgs @('-ConsumerRoot', $consumer, '-Role', $role, '-Task', $task, '-Workspace', $consumer, '-CoordinationDir', $coordination, '-FrozenInputArtifact', $frozenArtifact)
    Assert ($single.effective_mode -eq 'single' -and $single.directive -eq 'execute_sequentially' -and -not $single.spawn_allowed -and -not $single.gateway_allowed) 'single project Role contract failed'

    Invoke-JSON -Path $core -CliArgs @('--enable', '--coordination-dir', $coordination) | Out-Null
    Invoke-JSON -Path $core -CliArgs @('--set-workspace-mode', '--workspace', $consumer, '--workspace-mode', 'framework', '--coordination-dir', $coordination) | Out-Null
    $policyPath = Join-Path $consumer 'policy.json'
    $policy = Get-Content -LiteralPath (Join-Path $fixture 'native-smoke\policy.json') -Raw | ConvertFrom-Json
    $policy.registered_bindings[0].workspace = $consumer
    $policy.role_routes.$role.primary.workspace = $consumer
    [System.IO.File]::WriteAllText($policyPath, (($policy | ConvertTo-Json -Depth 12) + [Environment]::NewLine), [System.Text.UTF8Encoding]::new($false))
    $framework = Invoke-JSON -Path $entry -CliArgs @('-ConsumerRoot', $consumer, '-Role', $role, '-Task', $task, '-Workspace', $consumer, '-StateDir', $state, '-CoordinationDir', $coordination, '-PolicyFile', $policyPath, '-FrozenInputArtifact', $frozenArtifact)
    Assert ($framework.effective_mode -eq 'framework' -and $framework.directive -eq 'framework_controller' -and $framework.gateway_allowed) 'framework project Role contract failed'
    Assert ($framework.route.requested_instances -eq 1 -and $framework.route.actual_instances -eq 1) 'project Role dispatch did not create exactly one instance'
    Assert ($framework.route.plan.steps[0].role -eq $role -and $framework.route.plan.steps[0].task -eq $task) 'project Role task was not frozen into the one-step plan'
    Assert ($framework.route.plan.steps[0].frozen_inputs.Count -eq 1) 'framework project Role did not freeze the read-only input'

    $disabled = Invoke-JSON -Path $core -CliArgs @('--disable', '--coordination-dir', $coordination)
    $afterDisable = Invoke-JSON -Path $entry -CliArgs @('-ConsumerRoot', $consumer, '-Role', $role, '-Task', $task, '-Workspace', $consumer, '-CoordinationDir', $coordination, '-FrozenInputArtifact', $frozenArtifact)
    Assert (-not $disabled.framework_enabled -and $afterDisable.effective_mode -eq 'direct') 'project Role disable fallback contract failed'

    $promptPath = [string]$direct.role_prompt_path
    $promptRaw = Get-Content -LiteralPath $promptPath -Raw
    [System.IO.File]::AppendAllText($promptPath, "`nTAMPERED", [System.Text.UTF8Encoding]::new($false))
    $failedClosed = $false
    try { & $entry -ConsumerRoot $consumer -Role $role -Task $task -Workspace $consumer -CoordinationDir $coordination | Out-Null }
    catch { $failedClosed = $true }
    Assert $failedClosed 'tampered project Role Prompt did not fail closed'
    [System.IO.File]::WriteAllText($promptPath, $promptRaw, [System.Text.UTF8Encoding]::new($false))

    [pscustomobject]@{
        status = 'passed'
        zero_install = $zero.directive
        direct = $direct.directive
        single = $single.directive
        framework = $framework.directive
        disable = $afterDisable.effective_mode
        role = $framework.route.plan.steps[0].role
        requested_instances = $framework.route.requested_instances
        actual_instances = $framework.route.actual_instances
        pinned_collaboration_path = $direct.collaboration_path
        tamper_fail_closed = $failedClosed
        frozen_input_sha256 = $framework.route.plan.steps[0].frozen_inputs[0].sha256
    } | ConvertTo-Json -Depth 5
}
finally {
    foreach ($path in @($consumer, $coordination, $state, $frozenRoot)) {
        if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path -Recurse -Force }
    }
}
