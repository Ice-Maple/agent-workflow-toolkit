---
name: multi-agent-orchestration
description: Use automatically when a business Skill provides collaboration.yaml, or explicitly for the advanced Gateway loop. Routes role-first plans across framework/direct/single, executes core-signed Codex App Native actions or the public external CLI bridge, and keeps business approval separate from technical release.
---

# Multi-Agent Orchestration Controller

This Skill is the framework Router/Controller. A user normally calls a business Skill or names a registered project Role, not this Skill. The business Skill owns its full workflow and optional `collaboration.yaml`; Workflow/project Role Packages own complete Role Prompt templates; this Skill owns only generic routing and host execution.

## Role-first entry

During the host's normal Skill/Role dispatch decision:

1. Treat `.agents/agent-workflow-role-first-entry.json` as the managed business-Skill registry. When the selected business Skill is registered, invoke `invoke_business_skill.ps1`; do not require the user or the business Skill to repeat orchestration logic or explicitly name this Controller.
2. Treat `.agents/agent-workflow-project-role-entry.json` as the managed project-Role registry. When the user or active project instruction asks for a registered Role such as `package/reviewer`, invoke `invoke_project_role.ps1` with the requested task. The project must not create a development Skill merely to reach that Role.
3. Both entries validate `.agents/agent-workflow-toolkit.lock.json` and their package receipt, then call only the canonical absolute binary paths recorded there. Never use PATH, a global binary, or another checkout as fallback. These are managed host-policy contracts discovered at task start, not OS-level hooks that can intercept a host which did not discover this Controller.
4. For a business Skill, an absent registry preserves its original body (`zero_install`). For a project Role, an absent registry leaves dispatch to the host's ordinary path. Once a matching managed entry is found, any receipt, hash, route, preflight, execution, or evidence failure is fail closed.
5. Honor the returned `effective_mode` for either entry:
   - `framework`: use the returned frozen run/instances and continue through TaskGateway;
   - `direct`: use the host's ordinary delegation path; for a project Role, load the returned verified Role Prompt and append the requested task;
   - `single`: do not spawn or call Gateway; the current Agent executes the task using the returned verified Role Prompt.
6. A business Skill sidecar may contain only `role_dependencies` and steps with `id`, `role`, `task`, `count`, and `after`. A project Role entry creates exactly one step with `count=1`; Adapter/model/Gateway fields still come only from machine-local policy. When the caller supplies `FrozenInputArtifact`, preserve the returned receipt in zero-install/direct/single and the frozen plan in framework; never silently discard it.
7. In framework mode, call `prepareCollaborationInstance` only for returned ready instances, run the selected host preflight, then call `prepareAttempt` with the exact `run_id`, `instance_id`, binding, and gate epoch. A Cursor project Role with frozen input must use a machine-local `ask + read-only` profile; `plan`, another Adapter, a changed hash/root, or an undeclared sibling fails preflight rather than falling back.
8. For Codex App Native, execute the core-signed action with the App's native capability. For `claude`, `cursor`, or `codexcli`, call the receipt-pinned bridge `--execute`; never compose Provider shell commands in the business repository.
9. After validated completion, call `recordCollaborationTechnicalRelease`. This may advance only the frozen DAG. It must not create `HumanDecision`, `user-input:*`, `goal:*`, or business approval.
10. Once framework admission is selected, any role/route/preflight/permission/workspace/MCP, technical-release, contamination, outcome-unknown, cancellation, retry/fallback, or `delivery_missing` error is fail closed. Never retry by switching to `direct`.

## Non-negotiable boundaries

1. The Go TaskGateway is the only source of truth for Task, WorkflowNode, Attempt, HostAction, completion, HumanDecision, and Event state.
2. Call the core through the versioned protocol and `agent-workflow-core --gateway`. Never edit a snapshot or event file directly.
3. A Native subagent may run only after the core has returned a pending host action. Never launch a subagent merely because a node was proposed or because an Agent text says it is ready.
4. Codex App Native uses the App's native subagent capability. Do not shell-launch `codex`, `codex.exe`, or another CLI to simulate the Native path.
5. Do not use `--force`, `--yolo`, hidden approval, automatic Goal authority, or an unregistered binding to bypass a checkpoint.
6. If the selected host capability is unavailable, report `unsupported` or `degraded` through the core path. Any route fallback must be selected by machine-local policy; do not silently switch Adapter after Attempt admission.
7. In a consumer repository, require a valid schema-v2 `.agents/agent-workflow-toolkit.lock.json` produced by `tools/orchestration/install_controller_skill.ps1`; the managed entry runs its `-Check` mode before routing. Do not silently use PATH or another global/copied binary or Skill.
8. Treat the node role as admission data. The proposed node role and binding role must be the same non-empty configurable string.

## Advanced Gateway loop

For each user task:

1. Preserve the user's objective and Workflow proposal as input. Create a Task, propose explicitly named nodes, and run binding/workspace/MCP preflight.
2. Prepare an Attempt only after preflight succeeds. Show the core's dispatch prompt exactly, including role, Adapter, model, and Reasoning.
3. Poll host actions. For each pending action, verify the action ID, Attempt ID, sequence, lease, parameters hash, workspace, and task canary before invoking the Native capability.
4. Execute only the Gateway v1 public actions `launch`, `observe`, or `cancel_host`. Submit the structured result once with the exact lease. `send` and `cleanup` are reserved Adapter lifecycle primitives and are not valid Gateway HostActions.
5. Preserve the Native task handle, provider/session identity, workspace, and task canary. A mismatch is contamination or incomplete evidence according to the core response; stop and surface the human checkpoint.
6. Request completion through the core. Required artifacts must exist at the declared canonical path and pass core validation. Empty text, a successful turn event without final text, or tool-only output is not completion evidence.
7. For a legacy node, when the core returns `awaiting_human_decision`, stop and ask for the applicable real decision. For a collaboration instance, request technical release only under the role-first conditions above.
8. Only a real HumanDecision may advance a legacy node; only a valid CollaborationTechnicalRelease may advance a collaboration DAG. Keep all historical Attempts, releases, and decisions visible through core responses.
9. If completion returns `delivery_missing`, do not wait for a `send` action or recompute completion after writing a local artifact: Gateway v1 will not issue one and the checkpoint is immutable. Request `request_revision` or `abort`; a new revision/node is the supported recovery path.
10. Release provider process/task resources in the host or Adapter runner's `finally` path. Cleanup is not submitted as a Gateway HostActionResult.

## Native action result contract

The action/result mapping and idempotency rules are documented in [references/native-action-contract.md](references/native-action-contract.md). The reference is a contract, not a substitute for the core response. If the contract and a live core response disagree, stop with a diagnostic rather than guessing.

## Minimal Native reference

Use a read-only explorer/reviewer node that produces a Markdown artifact in the task workspace. Do not write to a business repository, enable Goal mode, or expand permissions in the first Native smoke. The expected sequence is:

```text
createTask → proposeNode → runPreflight → prepareAttempt
  → poll/execute core-signed Native actions → requestCompletion
  → awaiting_human_decision → user approve
```

This legacy sequence must leave the task at the human checkpoint. A role-first collaboration instead uses the dedicated technical-release operation after validated completion; `delivery_missing` still requires a real revision/abort decision and cannot be technically released.
