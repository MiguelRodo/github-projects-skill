# Consumed administration metadata in direct processing

After the deterministic executor has independently verified every requested
administrative action, required review and queue completion, it automatically
attempts to remove consumed issue-body metadata. Agentic execution is unchanged;
this is not a historical-body migration or a new source of authority.

Recognised forms are the ``Administrative setup for `pj -i`:`` bullet trailer,
the `Project administration` Markdown heading, and the bold or plain
`Project administration requested:` paragraph. Exactly one unfenced block is
required. Only explicit Project, Sub-project/subproject, Parent/Native parent,
Class/Type, Priority, Status, Due date and Target date metadata is eligible.
Unknown or duplicate settings, multiple blocks and malformed values retain the
block. Named `Example Project #4` references must match the checked contract.

All parsed settings are checked against fresh target membership, exact bound
fields/options, labels only for declared label storage, and native parent state.
An unsatisfied or unavailable setting retains the entire block. This does not
silently fall back from a missing expected field.

Mixed substantive restrictions are retained byte-for-byte, including instructions
to keep an issue open, avoid sending an email, defer implementation, or leave
Priority unset until a later planning decision. A heading carrying remaining
restrictions stays in place. No marker-to-end-of-body deletion is performed;
neighboring task prose, separators, whitespace and unrelated sections remain
unchanged. Authority comments and execution receipts are never removed.

Before the single supported issue edit, write a private recoverable original,
exact proposed spans and hashes under `$XDG_STATE_HOME/pj/body-cleanup` (or
`~/.local/state/pj/body-cleanup`). Then immediately re-read the body and compare
the original text and hash. Any observed concurrent edit aborts cleanup. Verify
the exact resulting body separately after editing. A failed or mismatched
readback never causes a blind overwrite, automatic restoration or retry; inspect
the receipt and original record first. Protect these records as private issue
content, and do not commit them.

The receipt's `bodyCleanup` reports the separate cleanup result. Administration
may be verified while metadata is retained or its edit outcome is uncertain;
the executor prints a warning in those cases. `not_present` is an idempotent
no-op. Failures, partial action application and pending reviews never reach
this post-success edit.

GitHub does not document atomic conditional issue-body writes. The fresh
read/hash guard detects observed changes but leaves a residual race between
that read and the PATCH; an exact readback cannot prove no edit was overwritten
inside that interval. No compare-and-swap or absolute concurrency guarantee is
claimed. See the [official conditional-request guidance](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api#use-conditional-requests).
