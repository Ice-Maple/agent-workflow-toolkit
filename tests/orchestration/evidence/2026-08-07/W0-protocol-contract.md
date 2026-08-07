# Wave 0 protocol and environment evidence

- Date: 2026-08-07
- Scope: protocol v1 shape, fake Adapter/Native host-action contract, local toolchain and CLI version probes
- Real Provider execution: not performed

## Toolchain probe

The current Codex process does not yet inherit the newly configured user PATH, so the Go toolchain was invoked by absolute path:

```text
E:\Program Files\Go\bin\go.exe version
go version go1.26.5 windows/amd64

C:\Users\shterm\go\bin\gopls.exe version
golang.org/x/tools/gopls v0.23.0

C:\Users\shterm\go\bin\dlv.exe version
Delve Debugger
Version: 1.27.1
```

The current process reports `gopls` and `dlv` as unavailable by bare command name. This is an environment refresh issue, not a missing binary; a later App restart can revalidate the user PATH.

## CLI version probe

Read-only version commands returned:

```text
cursor-agent --version
2026.08.04-aaa8809

claude --version
2.1.218 (Claude Code)

codex --version
codex-cli 0.144.1

node --version
v24.14.0

python --version
Python 3.11.9
```

These probes do not establish Adapter completion or capability support. Workspace trust, Windows sandbox, MCP isolation, stream/transcript delivery and real process cancellation remain Wave 3 smoke responsibilities.

## Go contract verification

Commands run from the repository root using `E:\Program Files\Go\bin\go.exe`:

```text
go test -count=1 ./...
?    agent-workflow-toolkit/cmd/agent-workflow-core [no test files]
ok   agent-workflow-toolkit/internal/adapters/fake
ok   agent-workflow-toolkit/internal/protocol/v1

go vet ./...
PASS

go build ./...
PASS
```

The protocol CLI also passed:

```text
agent-workflow-core --version
0.1.0-wave0

valid request through PowerShell stdin
{"protocol_version":1,"request_id":"wave0-request-001","ok":true,"payload":{"operation":"runPreflight","status":"protocol_valid"}}

unsupported protocol major
exit=1

unknown operation
exit=1
```

PowerShell 5.1 can add a UTF-8 BOM when piping text to a native process. The protocol decoder now accepts that transport marker and keeps the rest of the envelope strict; a regression test covers this boundary.

## Wave 0 status

The protocol and no-side-effect fake contract are implemented and locally verified. Wave 0 is not complete until the Native capability probe and the remaining real Adapter preflight probes are reviewed at the next human checkpoint.
