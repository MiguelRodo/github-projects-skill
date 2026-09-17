# Repository contract

Repository-specific GitHub Project configuration lives under `.projects/`. A contract records facts that differ between repositories or Projects. Common behaviour, default vocabularies and presentation defaults belong in the shared `github-projects` skill.

The consequence is deliberate: when a standard row or section is absent, the current shared skill default applies. Add contract content only when a Project intentionally differs from that default.

## Source precedence

Use a local replacement skill only when this exact file exists:

```text
.projects/skills/github-projects/SKILL.md
```

For backward compatibility, `.projects/skills/github-project-admin/SKILL.md` is also recognised. The `.projects/` directory by itself never overrides the canonical skill. Always read `.projects/project.md` after selecting the skill.

## Single-Project form

A normal single-Project contract contains identity, routing, privacy/governance and, until the shared field-profile setup in #199 is available everywhere, the provider field locations needed by the current mutation path:

```markdown
# GitHub Project configuration

| Key | Value |
| --- | --- |
| Contract version | 1 |
| Mode | single |
| Issue repository | octo-user/example |
| Project owner | octo-user |
| Project number | 12 |
| Project title | Example planning |
| Routing | linked repository |
| Privacy | repository |

## Field locations

| Common dimension | Provider location | Provider field |
| --- | --- | --- |
| Class | project field | Class |
| Priority | project field | Priority |
| Status | project field | Status |
| Due date | project field | Due date |

## Governance

- Collaboration mode: solo administration in a private repository.
```

Do not add default rows merely to make the contract self-contained. In particular, normal contracts do not need `Issue write-up style | tidy`, `Issue prose style | natural-direct`, `Chat implementation label | pj:implement-chat`, Class values, a Priority mapping, a Status mapping or palette tables.

An optional `Owner type` row may assert `user` or GitHub's provider spelling `organization`; setup fails if that assertion disagrees with the live owner.

A resolved contract must state its collaboration mode explicitly, for example `Collaboration mode: collaborative administration in a shared repository.` or `Collaboration mode: solo administration in a private repository.` A dispatcher may instead carry `Governance | personal` or `Governance | collaborative`; `shared` is a legacy spelling of `collaborative`. Missing, generic, contradictory or unrecognised governance is treated as collaborative.

## Shared semantic defaults

### Class / Issue Type

When no `Class values` section is declared, use the shared vocabulary:

- `Task`
- `Bug`
- `Enhancement`
- `Data`
- `Analysis`
- `Deliverable`
- `Documentation`
- `Epic`

`Task` is the ordinary fallback. Native GitHub parent/sub-issue relationships carry hierarchy independently of Class.

A repository may deliberately declare a smaller or different set with an explicit `## Class values` table. That section is an override, not required boilerplate.

### Priority

When no `Priority mapping` section is declared, use the common values directly:

| Common value | Provider value |
| --- | --- |
| P0 | P0 |
| P1 | P1 |
| P2 | P2 |
| P3 | P3 |

A repository may declare a complete one-to-one mapping when its provider names intentionally differ. All four common values must appear exactly once and map to distinct non-empty provider values.

During legacy or incomplete onboarding, an explicit section may still say:

```markdown
## Priority mapping

Priority mapping status: pending
```

That marker disables Priority administration until the live field is inspected. Absence of the entire section is different: it means the shared P0-P3 default.

### Status

When no `Status mapping` section is declared, the common lifecycle is `Todo`, `In progress`, `Done`. The implementation may normalise obvious spelling/spacing variants such as `To do`, `in-progress` or `completed`, then validates the resulting provider option against live state before writing.

Declare an explicit Status mapping only when a Project intentionally uses a different lifecycle vocabulary.

### Option colours

Standard palettes are skill/setup defaults, not repository contract state. A contract may still make a palette exact when a Project genuinely requires a local presentation override:

```markdown
## Class values

| Option | Colour |
| --- | --- |
| Task | YELLOW |
| Bug | RED |
```

Supported GitHub colours are `BLUE`, `GRAY`, `GREEN`, `ORANGE`, `PINK`, `PURPLE`, `RED` and `YELLOW`. Only an explicitly declared palette is a contract constraint.

## Optional behavioural overrides

### Chat implementation label

The local Chat-to-`pj` handoff defaults to `pj:implement-chat` when the row is absent. A resolved Project contract may use another non-empty label for a genuine local reason or explicitly disable the handoff with:

```markdown
| Chat implementation label | disabled |
```

For a multi-Project repository, put an override in the resolved child contract rather than the dispatcher root.

### Issue write-up style

`tidy` is the default when the row is absent. Supported explicit overrides are:

- `direct`: derive only the structural title/body needed, plus spelling and grammar corrections;
- `tidy`: reword and organise supplied material without adding substantive information;
- `unrestricted`: add useful grounded structure/detail when helpful.

### Issue prose style

`natural-direct` is the default when the row is absent. It uses plain, precise UK English and useful GitHub Markdown without templated AI prose. Other values are unsupported until the shared skill defines them.

## Multi-Project form

Use `.projects/project.md` as a dispatcher:

```markdown
# GitHub Project dispatcher

| Key | Value |
| --- | --- |
| Contract version | 1 |
| Mode | dispatcher |
| Issue repository | octo-user/issues |
| Privacy | private repository |
| Governance | personal |

## Routes

| Project key | Routing label | Project number | Contract |
| --- | --- | --- | --- |
| alpha | project:alpha | 4 | .projects/projects/alpha.md |
| beta | project:beta | 5 | .projects/projects/beta.md |
```

Each referenced child uses the single-Project form with `Mode | project` and a `Project key` metadata row. Its key, `label:` routing value, Project number and issue repository must match the dispatcher row exactly. Route keys, routing labels and Project numbers must each be unique.

A zero-route dispatcher is a valid saved onboarding state but cannot resolve ordinary administration.

## Field locations

Field locations tell the current mutation implementation where a semantic dimension physically lives, for example a Project field, organisation Issue Type or organisation issue field. They are provider bindings, not value/palette contracts.

Issue #199 owns inferring the standard user-versus-organisation field profile from live ownership and creating/reconciling the standard fields. Until that path is implemented and active contracts are migrated, `Field locations` remains required for ordinary mutation compatibility. Explicit field-location rows will continue to be valid afterwards as deliberate provider overrides.

Do not store transient GraphQL node IDs, REST option IDs or credentials. Discover IDs and live options at operation time.

## Governance and source rules

Record only local constraints, for example:

- whether issues may contain private material;
- whether administration is solo or collaborative;
- whether assignment defaults exist;
- whether a source must be consulted before inventing or restructuring scope;
- whether routing or sub-project labels are required;
- which external mirror is read-only.

Do not repeat fresh inspection, narrow writes, stale refusal, preservation, readback, native hierarchy or other shared operating rules. The skill owns them.

## Exceptional setup

Use `.projects/setup.sh` only for prerequisites unique to this repository. The shared `scripts/setup.sh` discovers it automatically from the repository root.

By default the local script extends shared setup and runs after common GitHub checks. To replace common setup completely, put this exact marker within the first 20 lines:

```bash
# github-projects: override
```

For backward compatibility, `# github-project-admin: override` is also recognised. Keep local setup idempotent and never store credentials in it.
