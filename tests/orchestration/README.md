# Orchestration tests

This directory contains contract, integration, and real Adapter smoke tests for the multi-agent orchestration MVP.

Wave 0 currently covers only the versioned protocol and the no-side-effect fake Adapter. It must not be reported as a real Provider smoke test.

Run the Go checks from the repository root with the absolute toolchain path when the current shell has not inherited the updated PATH:

```powershell
& 'E:\Program Files\Go\bin\go.exe' test ./...
& 'E:\Program Files\Go\bin\go.exe' run ./cmd/agent-workflow-core --version
Get-Content tests/orchestration/fixtures/wave0/valid-request.json |
  & 'E:\Program Files\Go\bin\go.exe' run ./cmd/agent-workflow-core --validate-request
```

Wave evidence belongs under `tests/orchestration/evidence/<YYYY-MM-DD>/W*-*`. Do not store credentials, authentication headers, complete environment variables, or private user prompts in evidence.
