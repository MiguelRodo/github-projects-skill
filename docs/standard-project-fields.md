# Standard Project field setup

`projects project setup-fields` is the one-time setup and reconciliation command for the shared `github-projects` planning profile. It reads the resolved `.projects` contract to identify the Project and any deliberate local overrides; the ordinary Class, Priority and presentation defaults come from the shared skill.

The command plans by default:

```bash
projects project setup-fields
```

For a dispatcher, provide one exact route selector such as `--project-key`, `--routing-label` or `--project-number`. Identifiers supplied together must agree.

Apply and independently verify the planned setup with:

```bash
projects project setup-fields --apply
```

For user-owned Projects the standard profile uses Project-local `Class` and `Priority` single-select fields. Class uses `Task`, `Bug`, `Enhancement`, `Data`, `Analysis`, `Deliverable`, `Documentation` and `Epic`, with the shared GRAY, RED, GREEN, PINK, PURPLE, ORANGE, YELLOW and BLUE palette. Priority uses `P0`, `P1`, `P2`, `P3` with RED, ORANGE, YELLOW and PURPLE. The setup also ensures the shared profile's date fields and relies on GitHub's native Status and parent/sub-issue hierarchy.

For organisation-owned Projects, Class is the organisation's native Issue Type and Priority is the organisation-native Priority issue field. Creating or reconciling those definitions has organisation-wide scope, so ordinary `--apply` refuses to make those wider changes. When that wider mutation has been explicitly authorised, use:

```bash
projects project setup-fields --apply --allow-organization-schema
```

The operation inspects live schema before writing, creates only missing standard pieces, reconciles recognised standard or legacy Priority options, preserves unrelated fields and options, and performs a fresh post-write inspection. A successful apply therefore means the requested standard profile was independently observed after the mutations, not merely that GitHub accepted the write calls.

The plan describes each reconciliation from the live diff, for example `rename High->P1`, `recolour P1 GRAY->ORANGE`, added options, reordering or re-enabling an Issue Type. Reconciliation sends the existing option IDs so item values survive renames. The post-write inspection also requires every option ID the reconciliation kept; if GitHub regenerated them, which clears every item's value for those options, the apply fails loudly instead of reporting success. A failed apply lists the changes already applied and the step that failed; run the plan again to see the remaining work.

On an organisation-owned Project, an existing Project-local field named `Priority` blocks attaching the organisation Priority issue field. Rename or remove that Project field first; its values do not migrate to the issue field.

This setup is deliberately not continuous policy enforcement. Repository contracts may still declare deliberate local overrides, and running ordinary issue or Project administration does not silently rewrite field definitions or organisation schema.
