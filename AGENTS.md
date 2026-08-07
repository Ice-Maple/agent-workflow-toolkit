# Agent Guide for agent-workflow-toolkit

## Repository intent

- This is an intentionally extensible repository for reusable Agent capabilities, working methods, skills, infrastructure, and engineering practices.
- Current areas include generic skills, context lifecycle and compression, orchestration, adapters, tool/MCP integration, evaluation, testing, diagnostics, installation, and distribution.
- The list above is illustrative, not exhaustive. Do not reject a useful generic capability merely because it does not fit an existing category.
- Multi-agent orchestration is the first active milestone, not the permanent scope of the repository.

## Boundary with domain repositories

- Generic, cross-domain Agent capabilities belong here when they can maintain a clear boundary from product or business semantics.
- Domain-specific workflows, prompts, artifacts, and acceptance rules belong in their domain repositories.
- This repository must not import or depend on `doc-ai-toolkit` or another concrete domain repository.
- Domain repositories may install, invoke, or version capabilities from this repository.
- Avoid premature abstraction: a new capability may begin as a focused module or Skill and evolve when real reuse appears.

## Project-level working rules

- Preserve unrelated user changes and keep writes within assigned ownership.
- Keep credentials, tokens, authentication headers, and secret plaintext out of repositories and task artifacts.
- Do not make dangerous permission-bypass behavior a hidden default.
- Implementation and review should be performed by separate agents where practical. Reviewers inspect actual files, diffs, tests, and evidence.
- Give each capability an explicit scope, maturity/status, evidence, and proportionate validation strategy. CLI smoke tests are required for supported CLI paths, but are not a universal requirement for every Skill or research artifact.
- Keep milestone-specific constraints in that milestone's plan or module; do not turn them into permanent repository-wide prohibitions.

## Current active milestone: multi-agent orchestration MVP

The following rules apply to the current multi-agent milestone:

- Codex App native subagents use the App's native capability, not a shell-launched Codex CLI.
- Initial external CLI adapters are Claude Code, Cursor Agent, and Codex CLI.
- Provider, model, reasoning, role, MCP, permission, and workspace values are configurable and must not become permanent closed enums.
- MCP is task/session allowlisted and does not default to all user-configured servers.
- Task intent, execution attempts, and completion decisions are separate records.
- Resume checks provider/session/workspace authority; identity mismatch becomes `contaminated`, not a normal retry.
- Each supported execution path requires a real smoke test; CLI help text alone is not completion evidence.

For this milestone, SQLite, dashboards, daemons, tmux UI, full mailbox/dead-letter infrastructure, OpenCode/Kilo adapters, and broad provider HOME/cache lifecycle management are out of scope. They may be reconsidered by a future capability or milestone; they are not repository-wide bans.

## Future capability examples

- Brainstorming Skill improvements
- Automatic context compression and summary quality
- Handoff, recovery, memory, and context-management practices
- Other reusable Agent skills and engineering capabilities

Treat these as examples and starting points, not a fixed roadmap or complete taxonomy.
