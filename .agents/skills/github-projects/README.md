# GitHub Project admin

This skill helps ChatGPT, Codex and other agents understand and manage a repository's GitHub issues and Projects. It checks the current state before changing anything and checks the result afterwards.

## Before you start

Install [GitHub CLI](https://cli.github.com/) 2.96 or newer, then check it:

```text
gh --version
```

If you have not signed in, run:

```text
gh auth login --web --scopes "project,read:org"
```

If you are already signed in but need Project access, run:

```text
gh auth refresh --scopes "project,read:org"
```

Check the result:

```text
gh auth status
gh api user --jq .login
```

The commands in this guide work in Bash, Git Bash and WSL. They also work from PowerShell when `bash` is installed and available as a command.

## Optional `projects` CLI

The `projects` Go CLI gives agents one tested command for supported repeated
operations. It validates `.projects/` contracts, reads complete Project item sets
with a count check, and supports plan-first issue and Project mutations:

```text
projects contract validate
projects project item-list --format json
projects issue create --title "Issue title" --apply
projects issue edit --issue 42 --add-label bug --apply
projects project item-add --issue 42 --apply
projects project item-edit --issue 42 --priority P1 --status "In progress" --apply
```

It is optional. The scripts below and direct GitHub operations remain supported.
Installation, APT setup and update checks are documented in the
[`projects` CLI guide](https://github.com/MiguelRodo/github-projects-skill/blob/main/docs/cli.md).

## 1. Create or find the GitHub Project

Open the **Projects** tab on your GitHub profile or organisation. Create the Project if needed.

Its number is after `/projects/` in the web address:

```text
https://github.com/users/example/projects/40       Project number 40
https://github.com/orgs/example-org/projects/12    Project number 12
```

Have the first relevant Project number ready. The initializer asks whether you want to add another after each Project.

## 2. Install and configure the repository

From the repository, run these one-line commands:

```text
gh skill install MiguelRodo/github-projects-skill github-projects --agent universal --scope project
bash .agents/skills/github-projects/scripts/init-project.sh
```

The initializer first explains that it will configure the repository so chats and agents can understand the Project. It discovers GitHub facts and asks only about collaboration, where issues are tracked (defaulting to the current repository), whether the repository uses one or several Projects, and the owner, number and routing identity of each Project you add.

This covers personal or collaborative repositories with one Project or several. Repository and Project privacy are discovered separately from GitHub.

For one Project, it creates `.projects/project.md` and adds a small section to `AGENTS.md`. It does not ask you to edit them or print the full contract.

For several Projects, it creates a validated empty dispatcher and offers to add one Project at a time. It discovers the live Project, writes the matching child contract, validates the combined configuration and then asks whether to add another. Rerunning the initializer continues the same flow without replacing current routes.

The initializer does not change live issues or Project fields. It leaves the existing Priority field and options alone, even if they do not include P3. Priority is marked as pending until an agent confirms its location, inspects the options and records a complete mapping.

It then asks whether it may stage, commit and push only the onboarding files. A failed commit or push leaves the work in a recoverable local state and prints the next command. Commit and push those files before using a remote chat or agent.

## 3. Use it from a chat interface

Create or open a [ChatGPT Project](https://chatgpt.com/projects), make the repository available to it, and paste this into the Project instructions:

> For work concerning a GitHub repository, especially reading or updating GitHub issues or Projects, first retrieve and follow the target repository's `AGENTS.md`. Follow the skill and configuration files it references. If the repository or `AGENTS.md` is unavailable, say so rather than guessing.
>
> Treat my prompt as the desired outcome. If this chat cannot make an authorised GitHub change, follow the repository's configured handoff. When its local Chat implementation queue is enabled, create the bounded queue issue and separate unedited authority comment described by the skill, and report the change as queued. Otherwise return the smallest executable command block with an independent result check.

Ask for the outcome you want. A specific change request supplies authority for
that change; broad organisation starts with a proposal for your approval.
The chat makes supported changes and uses the configured handoff for the rest.
The [provider instruction reference](references/provider-project-instructions.md)
has the reusable wording. Command handoffs should use ordinary commands and
should not change interactive shell options such as `set -e`, `set -u` or
`pipefail`.

## 4. Use it from an execution-capable agent

Codex cloud is one execution-capable option. Open [Codex environments](https://chatgpt.com/codex/settings/environments), create an environment and choose the repository. Use:

```text
bash .agents/skills/github-projects/scripts/setup.sh
```

Create a [classic GitHub personal access token](https://github.com/settings/tokens/new) with an expiry and the `repo`, `read:org` and `project` scopes. Authorise it for organisation SSO if required.

Add it to the Codex environment as an environment variable named `GH_TOKEN`, not a setup-only secret. Enable agent internet access and allow:

```text
github.com
api.github.com
```

See the [official Codex environment guide](https://developers.openai.com/codex/environments/cloud-environment) for how environment variables, setup and agent internet access work.

## 5. Start with the current issues

The initializer offers one shared, proposal-only first request after the chat and execution-capable agent instructions. For a resolved Project, the request can:

- confirm the local Priority location and mapping from the existing live field without changing it;
- set up or refine Issue Type or Class, with sensible colours;
- organise existing issues and useful native parent/sub-issue relationships;
- repair generic project-root, category-wrapper or standing issues where the existing structure obscures real outcomes;
- use body checkboxes for small local steps and sub-issues when work needs independent planning state;
- suggest optional sub-project labels only where they add value.

It does not authorise changes until you approve the proposal. After approval,
an execution-capable agent can apply and verify it; a chat that cannot complete
a change uses the configured queue, or minimal commands with readback.

## Add another Project

For an existing dispatcher, rerun
`bash .agents/skills/github-projects/scripts/init-project.sh` from the
repository root. It preserves existing routes and child contracts while
adding the new Project. Validate, review, commit and push the configuration,
then confirm the new Project's pending Priority mapping before using it.

For a single-Project contract, the initializer preserves the setup and exits.
Ask the agent to propose a conversion to a dispatcher while preserving the
existing Project's mappings, governance and membership. Approve that concrete
conversion before it is applied; do not delete the current contract to restart.
A Project in a different repository gets its own repository installation.

## Use the local administration queue

The standard resolved contract includes the historical
`Chat implementation label | pj:implement-chat`. Despite that label name, the
local `pj` queue is administrative-only. A chat may create a temporary handoff
for an authorised GitHub issue or Project mutation that it cannot finish.

Each handoff uses the configured label and a separate unedited
`PJ implementation authority:` comment establishing the bounded administrative
goal. Do not mark an implementation issue itself for queue execution.

Install `pj` from the
[pj operator guide](https://github.com/MiguelRodo/pj)
and keep the managed checkouts in its workspace. Run `pj -i`, or
`pj -i --repo example/repository` to select one managed issue repository. The local
agent may perform and verify the authorised GitHub/Project administration only.
Queue mode must never edit repository files, run implementation tests, create
implementation branches or pull requests, or otherwise implement product/code
work. Such work requires a separate explicit non-queue invocation.

See the [queue reference](references/local-implementation-queue.md) for authority,
discovery, readback and fallback rules. The optional
[`projects` CLI](https://github.com/MiguelRodo/github-projects-skill/blob/main/docs/cli.md)
performs supported GitHub operations; `pj` launches the agent that directs them.

## Issue Type / Class

Class or Issue Type says what kind of work item this is. A useful starter set is:

| Class / Issue Type | Preferred colour |
| --- | --- |
| Task | YELLOW |
| Bug | RED |
| Enhancement | GREEN |
| Data | PINK |
| Analysis | PURPLE |
| Deliverable | ORANGE |
| Documentation | GRAY |
| Epic | BLUE |

`Task` is the ordinary fallback when no more informative type adds useful meaning. `Data` covers source-data acquisition and stewardship as well as transformation, validation and production of derived analysis-ready data. `Deliverable` supersedes `Report`: use it for one bounded formal output or event that is handed over, submitted, presented, released, assessed or otherwise consumed as an output, including reports, manuscripts, presentations, posters, grant applications, protocols, handovers and software releases.

`Epic` is not the default for every top-level issue or every parent. A Task, Deliverable, Analysis or other type can have sub-issues and remain that type. Use Epic only when the broader coordination outcome is useful in its own right.

`Research` is not a default type. Ordinary exploratory work can usually be Task, analytical investigation can be Analysis, and development of an existing method or system can be Enhancement. Repositories may keep another local type when it carries a genuinely useful stable distinction.

The active model does not use Workstream as a standard dimension. Routing and optional sub-project labels say where the issue belongs; Class or Issue Type says what kind of work it is; native parent/sub-issue relationships carry hierarchy; Priority, Status and Due date carry planning state. Existing Workstream fields are legacy/unmanaged unless deliberately retained as non-standard metadata.

GitHub Milestones are optional temporal/checkpoint groupings for cases where several issues converge on the same release or submission. They are not a replacement Workstream field.

Preferred colours help repeated names look familiar across Projects, but colour is presentational. If there are more categories than distinct colours, reuse provider-supported colours.

See [Issue Type and Class design](references/issue-types.md) for the type meanings, hierarchy rules and migration guidance.

## Repository-specific setup

Each resolved Project contract can tune issue drafting with an optional metadata row:

```text
| Issue write-up style | tidy |
| Issue prose style | natural-direct |
```

Use `unrestricted` when the agent may add useful grounded detail, `tidy` when it may reword and organise supplied material without adding substantive information, or `direct` when it should do only the structural work needed to create the issue plus spelling and grammar corrections. `tidy` is the default when the row is absent, and an explicit instruction in the current request overrides the setting. In a multi-Project repository, put the row in the relevant `.projects/projects/*.md` child contract.

`Issue prose style` is separate from that content-level setting. `natural-direct` keeps titles and bodies plain, human-sounding and easy to scan in GitHub, with UK English, useful Markdown, preserved uncertainty and no generic AI scaffolding or inflated language. It applies regardless of whether the write-up style is `direct`, `tidy` or `unrestricted`.

If the repository needs extra tools, add `.projects/setup.sh`. It runs automatically after the shared setup and is not replaced when the skill is updated.

To replace common setup completely, place this within the first 20 lines:

```bash
# github-projects: override
```

## Update the skill

Run this inside the repository, then commit the changed skill files:

```text
gh skill update github-projects
```

The update does not replace `.projects/project.md` or `.projects/setup.sh`.

## If something fails

Paste the terminal output into the chat, or say which section failed. The agent should inspect what already succeeded and give you only the corrected or remaining commands.

If the failure is reusable, the agent may offer to improve `MiguelRodo/github-projects-skill`. It should open an issue or pull request only after you agree.
