---
name: multi-agent-orchestration
description: Use when coordinating a generic multi-agent Workflow through the shared Go TaskGateway and Codex App Native subagents. Creates explicit Task/Node/Attempt state, executes only core-signed Native host actions, preserves human checkpoints, and records durable completion evidence. Do not use for domain-specific prompts or business acceptance rules.
---

# Multi-Agent Orchestration Controller

This Skill is the Codex App Controller entrypoint for the Wave 2 Native slice. It coordinates generic Workflow semantics through the Go core; it does not become a second state machine and it does not contain provider-specific CLI commands.

## Non-negotiable boundaries

1. The Go TaskGateway is the only source of truth for Task, WorkflowNode, Attempt, HostAction, completion, HumanDecision, and Event state.
2. Call the core through the versioned protocol and `agent-workflow-core --gateway`. Never edit a snapshot or event file directly.
3. A Native subagent may run only after the core has returned a pending host action. Never launch a subagent merely because a node was proposed or because an Agent text says it is ready.
4. Codex App Native uses the App's native subagent capability. Do not shell-launch `codex`, `codex.exe`, or another CLI to simulate the Native path.
5. Do not use `--force`, `--yolo`, hidden approval, automatic Goal authority, or an unregistered binding to bypass a checkpoint.
6. If the host cannot provide the required Native capability, report `unsupported` or `degraded` through the core path and stop that execution path. Do not silently switch to an external CLI.

## Controller loop

For each user task:

1. Preserve the user's objective and Workflow proposal as input. Create a Task, propose explicitly named nodes, and run binding/workspace/MCP preflight.
2. Prepare an Attempt only after preflight succeeds. Show the core's dispatch prompt exactly, including role, Adapter, model, and Reasoning.
3. Poll host actions. For each pending action, verify the action ID, Attempt ID, sequence, lease, parameters hash, workspace, and task canary before invoking the Native capability.
4. Execute only `launch`, `observe`, `send`, `cancel_host`, or `cleanup`. Submit the structured result once with the exact lease. A missing, expired, or unknown outcome is not permission to retry or relaunch.
5. Preserve the Native task handle, provider/session identity, workspace, and task canary. A mismatch is contamination or incomplete evidence according to the core response; stop and surface the human checkpoint.
6. Request completion through the core. Required artifacts must exist at the declared canonical path and pass core validation. Empty text, a successful turn event without final text, or tool-only output is not completion evidence.
7. When the core returns `awaiting_human_decision`, stop. Ask the user for `approve`, `request_revision`, `retry_node`, `abort`, or the specific contamination release decision. Do not infer approval from a successful provider exit.
8. Only after the core records the human decision may the Controller propose or prepare the next node. Keep all historical Attempts and decisions visible through core responses.

## Native action result contract

The action/result mapping and idempotency rules are documented in [references/native-action-contract.md](references/native-action-contract.md). The reference is a contract, not a substitute for the core response. If the contract and a live core response disagree, stop with a diagnostic rather than guessing.

## First supported Workflow

Use a read-only explorer/reviewer node that produces a Markdown artifact in the task workspace. Do not write to a business repository, enable Goal mode, or expand permissions in the first Native smoke. The expected sequence is:

```text
createTask → proposeNode → runPreflight → prepareAttempt
  → poll/execute core-signed Native actions → requestCompletion
  → awaiting_human_decision → user approve
```

The Controller must leave the task at the human checkpoint when completion is valid. It must retain the first failed completion evidence if a core-issued delivery-only `send` follow-up is later permitted.
