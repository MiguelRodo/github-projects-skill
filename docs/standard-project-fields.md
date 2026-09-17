# Standard Project field setup

`projects project setup-fields` is the one-time setup and reconciliation command for the shared `github-projects` planning profile. It reads the resolved `.projects` contract only to identify the Project and any deliberate local overrides; the ordinary Class, Priority and presentation defaults come from the shared skill.

The command plans by default:

```bash
projects project setup-fields
```

For a dispatcher, provide one exact route selector such as `--project-key`, `--routing-label` or `--project-number`. Identifiers supplied together must agree.

Apply and independently verify the planned setup with:

```bash
projects project setup-fields --apply
```

For user-owned Projects the standard profile uses Project-local `Class` and `Priority` single-select fields. Class uses `Task`, `Bug`, `Enhancement`, `Data`, `Analysis`, `Deliverable`, `Documentation` and `Epic`, with the shared GRAY, RED, GREEN, PINK, PURPLE, ORANGE, YELLOW and BLUE palette. Priority uses `P0`, `P1`, `P2`, `P3` with RED, ORANGE, YELLOW and PURPLE. The setup also ensures the standard date fields required by the shared profile and relies on GitHub's native Status and parent/sub-issue hierarchy.

For organisation-owned Projects, Class is the organisation's native Issue Type and Priority is the organisation-native Priority issue field. Creating or reconciling those definitions has organisation-wide scope, so ordinary `--apply` refuses to make those wider changes. When that wider mutation has been explicitly authorised, use:

```bash
projects project setup-fields --apply --allow-organization-schema
```

The operation inspects live schema before writing, creates only missing standard pieces, reconciles recognised standard or legacy Priority options, preserves unrelated fields and options, and performs a fresh post-write inspection. A successful apply therefore means the requested standard profile was independently observed after the mutations, not merely that GitHub accepted the write calls.

This setup is deliberately not continuous policy enforcement. Repository contracts may still declare deliberate local overrides, and running ordinary issue or Project administration does not silently rewrite field definitions or organisation schema.
