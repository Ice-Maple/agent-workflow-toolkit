# Codex App Native capability probe

- Date: 2026-08-07
- Scope: current Codex App native subagent launch, follow-up delivery and interrupt
- Provider CLI: none
- Workspace writes: none

## Launch and first response

The Controller spawned a native subagent with a read-only instruction not to edit files, invoke external CLIs or use the network. The subagent returned:

```text
NATIVE_PROBE_STARTED
```

This confirms that the current App can create a native subagent task and receive its first result through the native collaboration surface.

## Follow-up delivery

The Controller sent one follow-up to the same native subagent. It returned:

```text
NATIVE_PROBE_FOLLOWUP
```

This confirms same-task follow-up delivery without spawning a second provider CLI session.

## Interrupt / cancel

A second native subagent was started with a bounded read-only wait and reported that it had begun the wait. The Controller then issued an interrupt while the agent was running:

```text
previous_status=running
final_status=interrupted
```

This confirms that the current App can interrupt a running native subagent task. No file, CLI or network operation was requested by that probe.

## Boundary of this evidence

The probe validates App-level launch, follow-up and interrupt primitives. It does not yet prove that the Go TaskGateway host-action bridge enforces `launch/observe/send/cancel_host/cleanup`, action leases, identity binding, cleanup confirmation, timeout state transitions or durable artifact completion. Those remain Wave 2 implementation smoke cases.
