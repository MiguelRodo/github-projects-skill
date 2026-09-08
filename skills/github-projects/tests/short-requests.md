# Short-request acceptance scenarios

These prompts deliberately contain only the desired outcome. An agent using the skill must supply the procedure.

## Exact mutation

Prompt:

> Set example#313 to P2.

Expected interpretation with the single-Project fixture:

- repository `octo-org/example`, issue 313, Project 12;
- common P2 maps exactly to organisation-native `Medium`;
- inspect the issue, membership, current native Priority and preservation set;
- re-read stale-sensitive state, update only Priority, then independently read it back;
- do not ask the user to repeat those procedural requirements.

## Ranked read

Prompt:

> What are the highest-priority open items in example?

Expected interpretation with the single-Project fixture:

- resolve Project 12 and its open member issues;
- read organisation-native Priority separately from Project fields;
- translate Urgent, High, Medium and Low to P0, P1, P2 and P3;
- rank P0 before P1 before P2 before P3 and report unresolved ties;
- make no mutation.

## Command-returning surface

Prompt:

> Set example#313 to P2.

If the surface cannot write, it must return one narrow organisation issue-field mutation using `Medium` plus an independent GET readback. It must state that the commands have not run.

## Issue creation styles

Prompt:

> Add an issue to chek whether the import preserves dates, because the dry run changed one.

Expected interpretation:

- `direct` derives a concise title and a description from the supplied statement, corrects `chek`, and does not otherwise rewrite, reorganise or expand it;
- `tidy` may reword and organise the supplied facts, but must not add acceptance criteria, implementation steps or other substantive information;
- `unrestricted` may add useful detail only when it is grounded in the request or a required project source;
- a more recent explicit instruction in the prompt overrides the configured style.

## Execution-surface behaviour

Use `agent-cli-choice.py` for isolated, observable agent trials. It copies the
current skill into a new synthetic repository and supplies fake `projects` and
`gh` executables. Neither fake tool contacts GitHub. Their command trace and
persisted state distinguish a real apply from a plan or a prose claim.

```bash
python3 skills/github-projects/tests/agent-cli-choice.py prepare edit /tmp/cli-choice-edit
```

Start a fresh execution-capable agent in that directory. Give it only this
request, without the expected tool choice or the checker implementation:

> Read operator-instructions.txt and follow its fixture isolation rules. Then
> use AGENTS.md to carry out the user request in prompt.txt. Execute the task
> and report the result. Do not modify fixture definitions or instructions.

Use a workspace sandbox with network access disabled for the agent's tools.
Its model connection may still require normal host access. Keep real provider
connectors unavailable. The fixture instructions require a PATH containing
only its fake providers and a small set of local utilities; appending the
system PATH would accidentally expose an installed real CLI in the unavailable
case. Do not relax the sandbox or enable automatic broad permissions to make
a trial pass.

After the agent finishes, independently inspect its answer and run:

```bash
python3 skills/github-projects/tests/agent-cli-choice.py check edit /tmp/cli-choice-edit
```

Repeat with a fresh directory and agent for each case:

| Case | Observable requirement |
| --- | --- |
| `read` | Complete inventory uses `projects project item-list`; supplementary issue reads are allowed. |
| `edit` | P2 is applied with `projects project item-edit --apply`, preserving the other fields. |
| `unavailable` | The CLI is absent; a direct provider read supplies the inventory. |
| `unsupported` | The installed CLI only supports reads; a direct field edit and separate readback complete the request. |
| `failure` | A supported apply returns a permission error; the agent stops without retrying a provider mutation. |

Record the agent/model, skill revision, case, actual commands, checker outcome
and whether the final answer matches the observed state. Run the cases on the
agents used by the operator when changing surface-selection guidance. The
offline regression suite checks the fixture and grader; it does not run agents
or prove instruction-following by itself. Never substitute static wording
matches or prewritten commands for a reported agent trial.

Observed on 2026-09-06 with GPT-5.6 Sol at Max effort in isolated Codex CLI
sessions using the prompt above:

| Case | Result |
| --- | --- |
| `read` | Passed: `projects project item-list --json`; supplementary `gh issue view` checked open state. |
| `edit` | Passed: `projects project item-edit --issue 313 --priority P2 --apply --json`; P2 read back and other fields preserved. |
| `unavailable` | Passed: direct `gh project item-list` with `--limit 1000`; no mutation. |
| `unsupported` | Passed: supported CLI reads retained; direct `gh project item-edit` followed by field-value readback. |
| `failure` | Passed: one failed CLI apply, no retry or fallback mutation, P1 preserved and the failure reported. |

These are bounded instruction-following results with synthetic provider state,
not a guarantee for every agent or a live-provider integration test.
