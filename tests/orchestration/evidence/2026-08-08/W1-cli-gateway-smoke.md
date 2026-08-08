# Wave 1 `--gateway` CLI smoke evidence

- Date: 2026-08-08
- Scope: real `cmd/agent-workflow-core --gateway` executable path
- Test: `cmd/agent-workflow-core/main_test.go:TestGatewayCLIExecutesRequestAndPersistsState`
- Status: passing

The automated smoke test builds the actual CLI executable and invokes it twice
with protocol-v1 JSON on stdin:

1. `createTask` with a temporary workspace and explicit `--state-dir`.
2. `runPreflight` for a missing node, expecting the structured `unknown_node`
   error response.

The test decodes both stdout responses with the protocol decoder, verifies the
task and revision IDs, and checks that the first invocation created a valid
`tasks/<task_id>/snapshot.json` and an `events.jsonl` containing
`task.created`. It also asserts that the executable exits successfully and
writes no unexpected stderr. The test uses only temporary directories and
contains no credentials, authentication headers, or private prompts.

This is executable-path evidence, not CLI help or `--version` evidence.
