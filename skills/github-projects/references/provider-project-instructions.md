# Provider Project instructions

Use this reference when configuring an ordinary ChatGPT Project or another provider's persistent instruction surface.

Keep the instructions small. They route to the target repository's own starting point; they do not repeat the procedure.

Suggested text:

```text
For work concerning a GitHub repository, especially reading or updating GitHub
issues or Projects, first retrieve and follow the target repository's
AGENTS.md. Follow the skill and configuration files it references. If the
repository or AGENTS.md is unavailable, say so rather than guessing.

Treat the user's prompt as the desired outcome. If this surface cannot perform
an authorised GitHub change, follow the repository's configured handoff. When
the local Chat-to-pj queue is enabled, mark the existing task issue for bounded
administrative reconciliation, or create a temporary handoff issue, and add the
authority comment described by github-projects, stating the administrative delta
itself rather than deferring to the issue body. Report it as queued, not
completed. Queue mode is administrative-only by effect: it never authorises
repository implementation, and ordinary task prose in a queued issue must not
stop that issue's administration. Otherwise return the smallest executable gh
command block with independent readback. Do not ask the user to restate the
skill's operating procedure.
```

Each repository normally installs the shared skill under `.agents/skills/` and routes to it from `AGENTS.md`. A repository may deliberately point elsewhere, but provider instructions should not hard-code that internal path.

Routine prompts should remain short:

- `Set example#313 to P2.`
- `What are the highest-priority open items in example?`

For the first broad organisation request, use the same proposal-only wording in
a chat interface or an execution-capable agent. Ask it to inspect and propose
the exact organisation, and do not authorise live changes until the operator
approves. After approval, a capable agent may execute and verify. A chat that
cannot write should use the configured local queue when available, otherwise
return the smallest executable command block with readback.

If a routine prompt must repeat stale checks, narrow mutation, preservation, verification or queue trust rules, move that missing behaviour into the skill instead of lengthening the standing instructions or prompt.
