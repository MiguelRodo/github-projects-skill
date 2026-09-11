# GitHub Projects made easier

This repository provides one shared skill for ChatGPT, Codex and other agents that work with GitHub issues and Projects. Once a repository is set up, ordinary requests can stay short:

- `What should I work on next?`
- `Set example#313 to P2.`
- `Organise the existing issues and show me what changed.`

The optional [`projects` Go CLI](docs/cli.md) handles repeated operations that
benefit from one tested implementation. It validates the `.projects/` contract,
reads complete Project item sets, and supports issue and Project item changes
with a plan before applying them and an independent check afterwards. It can
also report whether a newer release exists.

The [public guide](https://miguelrodo.github.io/projects/) covers both
this repository and `projects`, which coordinates project setup and
supplies the local `pj` agent launcher.

Release maintainers should use the [Go release and APT guide](docs/releasing.md).

## Before you start

You need Git, Bash and [GitHub CLI](https://cli.github.com/) 2.96 or newer on the computer where the repository is checked out. Check GitHub CLI with:

```text
gh --version
```

The commands below are one line each. They work in Bash, Git Bash and WSL. They also work from PowerShell when `bash` is installed and available as a command.

Sign in to GitHub CLI if you have not already done so:

```text
gh auth login --web --scopes "project,read:org"
```

If you are already signed in but have not granted Project access, add the permissions instead:

```text
gh auth refresh --scopes "project,read:org"
```

Then check the active login:

```text
gh auth status
gh api user --jq .login
```

The initializer checks authentication and Project access again before asking any questions.

## 1. Create or find the GitHub Project

Open the **Projects** tab on your GitHub profile or organisation. Create the Project if it does not exist yet.

Keep the Project number handy. It is the number after `/projects/` in the web address:

```text
https://github.com/users/example/projects/40       Project number 40
https://github.com/orgs/example-org/projects/12    Project number 12
```

If one repository manages several Projects, have the first Project number ready. The initializer will add it and then ask whether you want to add another.

## 2. Install the skill in the repository

Open a terminal in the repository and run:

```text
gh skill install MiguelRodo/github-projects-skill github-projects --agent universal --scope project
bash .agents/skills/github-projects/scripts/init-project.sh
```

Before the questions begin, the initializer explains what it will configure. It discovers the repository, its privacy, the Project title and whether its owner is a GitHub user or organisation. It asks only:

- whether other people work with you on the Project;
- whether the repository uses one Project or several;
- for each Project you add, its owner, number, Project key and routing label.

This covers personal or collaborative repositories with one Project or several. Repository and Project privacy are discovered separately from GitHub.

For one Project, it creates `.projects/project.md` and adds a small starting section to `AGENTS.md`. It does not print the whole contract or ask you to edit either file.

For several Projects, it creates a validated dispatcher first. It then offers to add one Project, discovers the live Project, asks for the routing identity, creates its child contract and asks whether to add another. If you stop before adding one, the empty dispatcher is saved safely but cannot yet resolve ordinary Project requests. Rerun the initializer to continue.

The initializer never changes live issues, fields or Project options. In particular, it leaves Priority exactly as it is. The initial contract marks its location and meaning as pending until an agent inspects the existing field and records a complete mapping. It will not add P3 or any other option during onboarding.

Finally, it asks whether it may stage, commit and push only these onboarding files. If committing or pushing fails, the local work is kept and the initializer prints the command to continue. Saying no simply leaves the files for you to review. Commit and push the installed skill and configuration before expecting a remote chat or agent to retrieve them.

## 3. Set up a chat interface

Create or open a [ChatGPT Project](https://chatgpt.com/projects), make the GitHub repository available to it, and paste this into the Project instructions:

> For work concerning a GitHub repository, especially reading or updating GitHub issues or Projects, first retrieve and follow the target repository's `AGENTS.md`. Follow the skill and configuration files it references. If the repository or `AGENTS.md` is unavailable, say so rather than guessing.
>
> Treat my prompt as the desired outcome. If this chat cannot make an authorised GitHub issue or Project administration change, follow the repository's configured handoff. When its local Chat queue is enabled, create the bounded temporary administration handoff and separate unedited authority comment described by the skill, and report the change as queued. Never use the queue to authorise repository implementation. Otherwise return the smallest executable command block with an independent result check.

You can then ask for the result you want. A request for a specific change supplies
authority for that change. For broad organisation, ask for a proposal first and
approve it before the chat applies it. The chat performs supported changes and
uses the configured handoff for anything its GitHub connection cannot do.

The [provider instruction reference](skills/github-projects/references/provider-project-instructions.md)
contains the reusable wording. For the full Drive, registry and ChatGPT setup,
start with the [project guide](https://miguelrodo.github.io/projects/).

## 4. Set up an execution-capable agent

Codex cloud is one execution-capable option. Open [Codex environments](https://chatgpt.com/codex/settings/environments), create an environment and choose the repository.

Use this setup command:

```text
bash .agents/skills/github-projects/scripts/setup.sh
```

Create a [classic GitHub personal access token](https://github.com/settings/tokens/new), give it an expiry, and select the `repo`, `read:org` and `project` scopes. If the organisation uses SSO, authorise the token for that organisation.

In the Codex environment:

- add the token as an environment variable named `GH_TOKEN`, not a setup-only secret;
- enable internet access during the agent phase;
- allow `github.com` and `api.github.com`.

Environment variables remain available while the agent works, whereas setup-only secrets do not. The [official Codex environment guide](https://developers.openai.com/codex/environments/cloud-environment) explains these settings.

## 5. Give it a useful first request

After the chat interface or execution-capable agent is ready, the initializer gives you one proposal-only request tailored to a resolved Project.

The request asks the surface to inspect existing issues, confirm the pending Priority location and mapping without changing the live field, propose useful Issue Type or Class values, preserve useful definitions and colours, organise the issues, build useful native parent/sub-issue relationships, repair generic root or category-wrapper issues, choose checkboxes versus sub-issues based on whether work needs independent planning state, and suggest optional sub-project labels only where they add value. It explicitly forbids live changes until you approve the proposal.

After approval, an execution-capable agent can apply and verify the proposal.
A chat that cannot complete a change uses the configured local queue when
available, or a command block with independent readback.

## Add another GitHub Project

If the repository already has a dispatcher (`Mode | dispatcher` in
`.projects/project.md`), rerun the initializer from its root:

```text
bash .agents/skills/github-projects/scripts/init-project.sh
```

It preserves existing routes and child contracts, discovers the additional
Project and asks for its key and routing label. Validate the result, review the
diff and commit and push the configuration through the repository's normal
workflow. Confirm the new Project's pending Priority mapping before using it.

For a single-Project contract, rerunning the initializer preserves that setup
and exits. Ask the agent to propose a conversion to a dispatcher that keeps the
existing Project's field mappings, governance and membership, then adds the
new route. Approve the concrete conversion before it is applied. Do not delete
the existing contract to start over. If the new Project belongs to a different
repository, install and initialise the skill there instead.

## Complete queued Chat administration locally

New resolved contracts include the historical
`Chat implementation label | pj:implement-chat`. Despite that label name, the
[local queue](skills/github-projects/references/local-implementation-queue.md)
is administrative-only. A chat may create a temporary handoff for an authorised
GitHub issue or Project mutation it cannot finish. It must not mark an
implementation issue itself for automatic repository work.

Each handoff needs the configured label and a separate unedited
`PJ implementation authority:` comment stating the bounded administrative goal.

Install the local launcher using the
[pj operator guide](https://github.com/MiguelRodo/pj),
keep the managed repository checkouts and contracts in its workspace, and run:

```text
pj -i
pj -i --repo example/repository
```

The optional selector limits processing to the exact managed issue repository.
The local agent checks authority, performs only GitHub/Project administration,
and independently verifies the result. Queue mode must never edit repository
files, run implementation tests, create implementation branches or pull
requests, or delegate such coding work. Repository implementation requires a
separate explicit non-queue invocation.

## Issue Type / Class defaults

The skill uses one classification dimension for the kind of work. The starter vocabulary is `Task`, `Bug`, `Enhancement`, `Data`, `Analysis`, `Deliverable`, `Documentation` and `Epic`.

`Task` is the ordinary fallback. Use a more specific type when the distinction improves planning. `Data` covers source-data acquisition and stewardship as well as transformation, validation and production of derived analysis-ready data. `Deliverable` supersedes `Report` and means one bounded formal output or event that is handed over, submitted, presented, released, assessed or otherwise consumed as an output. That includes reports, manuscripts, presentations, posters, submissions, grant applications, protocols, handovers and software releases.

`Epic` is deliberately narrow: use it for a broad coordination outcome that stays useful while several independently meaningful pieces of work are managed separately. Top-level issues do not need to be Epics, and parenthood does not imply Epic. A Deliverable, Analysis or Task may have children and keep its own type.

`Research` is not a default type. Ordinary exploratory or decision work can normally be a Task, analytical investigation can be Analysis, and work developing an existing method or system can be Enhancement. Repositories may still keep a deliberate local type when it adds useful meaning.

The active model does **not** use Workstream as a standard dimension. Routing or sub-project labels answer where the issue belongs; Issue Type/Class says what kind of work it is; native parent/sub-issue relationships carry scope and decomposition; Priority, Status and Due date carry planning state. Existing custom Workstream fields in older Projects are legacy/unmanaged unless a repository deliberately keeps them as non-standard metadata.

GitHub Milestones are optional for genuine shared checkpoints such as releases or submissions. They are not a replacement Workstream taxonomy, and a single formal output often needs only a Deliverable issue plus a due date.

Preferred colours make repeated types easier to recognise, but colour is presentational. Reuse provider-supported colours when there are more categories than distinct colours.

See the full [Issue Type and Class design guide](skills/github-projects/references/issue-types.md) for type meanings, hierarchy rules and migration guidance.

## Issue write-up styles

A resolved Project contract may set how much the agent reshapes an issue during creation or a substantial rewrite:

- `direct` derives the title and, when the supplied material supports one, a description, and fixes spelling and grammar without further rewriting or expansion;
- `tidy`, the default, may reword and organise supplied material but cannot add substantive information;
- `unrestricted` may add useful detail grounded in the request and required project sources.

The latest explicit instruction in the current request takes precedence. Existing contracts that use the retired `minimal` name should change it to `direct`.

## Issue prose style

A separate contract row controls how issue prose reads, regardless of the write-up level:

```text
| Issue prose style | natural-direct |
```

`natural-direct` is the default. It keeps GitHub titles and bodies plain, individual and easy to scan: UK English, natural sentence length, useful rather than templated Markdown, no added certainty, and no generic AI preambles or inflated wording. `Issue write-up style` still controls whether the agent is `direct`, `tidy` or `unrestricted`; the prose rule applies after that content boundary is respected.

## Update the installed skill

Run this in the repository and commit the changed files:

```text
gh skill update github-projects
```

Updating the shared skill does not replace `.projects/project.md` or a repository-specific `.projects/setup.sh`.

## Maintaining this repository

For `MiguelRodo/github-projects-skill` itself, the canonical skill is under `skills/`. Its Codex setup command is:

```text
bash skills/github-projects/scripts/setup.sh --install-skill-from .
```

The shorter operator guide is in [the skill README](skills/github-projects/README.md). [Issue #1](../../issues/1) contains the roadmap and [issue #67](../../issues/67) records the current architecture.

The [current architecture](docs/architecture/v1-boundaries.md) describes the
supported Markdown contracts and CLI. Files under `docs/spec/`, `schemas/` and
`examples/` retain the older `projectctl/v1` design as historical material; they
are not configuration instructions for `projects`.

Changes use GitHub issues and pull requests. See [CONTRIBUTING.md](CONTRIBUTING.md). This project uses the [MIT Licence](LICENSE).
