# Release `projects`

Releases use the Go-specific action in `MiguelRodo/actions`. The workflow creates
the semantic version tag and its floating aliases, runs GoReleaser, attaches
checksummed archives and Debian packages to the GitHub Release, then publishes
the `.deb` files to `MiguelRodo/apt-miguelrodo`.

The shared action currently runs GoReleaser 1.x, so `.goreleaser.yml` uses the
version 1 configuration format. Do not change it to the version 2 format until
the shared action is upgraded as well.

## One-time repository setup

The release workflow uses two Actions secrets. Configure these when enabling
publishing in a repository, or rotate them when needed:

- `APT_REPO_TOKEN`: a fine-grained token with Contents read and write access to
  `MiguelRodo/apt-miguelrodo`. The ordinary `GITHUB_TOKEN` cannot push to a
  different repository.
- `GPG_PRIVATE_KEY`: the ASCII-armoured private key whose public half is already
  published as `apt-miguelrodo/KEY.gpg`.

If the private key has a passphrase, also add
`GPG_PRIVATE_KEY_PASSPHRASE`. Do not paste any of these values into an issue,
commit, workflow file or agent prompt.

GitHub CLI can store the secrets without putting their values in the command
line. Run the first and third commands interactively; replace the key path in
the second command with the local private-key file:

```bash
gh secret set APT_REPO_TOKEN --repo MiguelRodo/github-projects-skill
gh secret set GPG_PRIVATE_KEY --repo MiguelRodo/github-projects-skill < /path/to/private-key.asc
gh secret set GPG_PRIVATE_KEY_PASSPHRASE --repo MiguelRodo/github-projects-skill
```

The passphrase secret may be omitted for an unprotected signing key. Check only
the configured secret names with:

```bash
gh secret list --repo MiguelRodo/github-projects-skill
```

## Publish a release

For routine releases, request a bump from the latest semantic version tag:

```bash
gh workflow run go-version-release.yml \
  --repo MiguelRodo/github-projects-skill \
  -f bump_type=patch
```

The action creates the tag before it runs GoReleaser. If a failed run has
already created the requested tag, do not rerun that same version after fixing
the commit: the action will refuse to move an existing tag. Use the next patch
version instead, or deliberately remove the stale tag after confirming that no
GitHub Release was published.

Use `minor` or `major` in the same way. For a first release in a repository with
no semantic version tags, or an intentional exact next version, supply
`-f version=X.Y.Z` instead of `-f bump_type=patch`, replacing `X.Y.Z` with the
chosen next version. Check the existing releases first:

```bash
gh release list --repo MiguelRodo/github-projects-skill
```

The workflow also accepts the inputs in the GitHub Actions web interface.
Watch a run with:

```bash
gh run watch --repo MiguelRodo/github-projects-skill
```

Do not set both `version` and `bump_type`. `version_force` is an escape hatch
for an intentional version jump or downgrade, and should normally remain
false.

## Verify the result

Check the GitHub Release, then verify that the signed APT repository contains
the same package version:

```bash
gh release view --repo MiguelRodo/github-projects-skill
curl -fsSL https://miguelrodo.github.io/apt-miguelrodo/dists/stable/main/binary-amd64/Packages \
  | awk '/^Package: projects$/{show=1} show{print} show && /^$/{exit}'
```

On a machine that has added the APT source, this read-only command shows the
installed and candidate versions without `sudo`:

```bash
apt-cache policy projects
```

GoReleaser configuration can be checked locally with:

```bash
goreleaser check
goreleaser release --snapshot --clean
```

Snapshot mode builds packages but does not publish them.
