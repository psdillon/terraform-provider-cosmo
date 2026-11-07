# About this fork

This repository is a fork of [wundergraph/terraform-provider-cosmo](https://github.com/wundergraph/terraform-provider-cosmo),
the Terraform provider for WunderGraph Cosmo. It adds features that the original ("upstream") project
doesn't have yet. The plan is to contribute them back upstream. Until they're merged and released there,
anyone who needs them has to use this fork's builds.

This file only exists in the fork, on the `release` branch. Keep it out of the branches used for pull
requests to upstream.

## What the fork adds

| Feature | Type | Branch | Status |
|---|---|---|---|
| `cosmo_organization_group`: create and manage organization groups (role-based permissions) | resource + data source | `feat/organization-group` | Code complete; data source tested against a real org |
| `cosmo_api_key`: create organization API keys and assign them to a group | resource | `feat/api-key` | Code complete; not yet tested against a real org |

Example:

```hcl
resource "cosmo_organization_group" "ci" {
  name        = "ci-publishers"
  description = "Publishes subgraphs from CI"
  rules = [
    { role = "subgraph-publisher" },
  ]
}

resource "cosmo_api_key" "ci" {
  name     = "ci-publisher"
  group_id = cosmo_organization_group.ci.id
  expires  = "1_year"
}

output "ci_api_key" {
  value     = cosmo_api_key.ci.key
  sensitive = true
}
```

Requirements and caveats:

- Groups need the RBAC feature enabled for the Cosmo organization. The `COSMO_API_KEY` that Terraform
  uses needs the `organization-admin` role.
- The API key's secret is returned only once, when the key is created. Terraform stores it in the state
  file, so treat the state as a secret.
- A rule's `resources` list needs target IDs for federated graphs and subgraphs. The existing
  `cosmo_federated_graph` and `cosmo_subgraph` resources don't expose those IDs yet, so for now scope
  rules with `namespaces`.

Full reference docs are in `docs/resources/` and `docs/data-sources/`, with examples in `examples/`.

## How the fork is organised

### Remotes

A *remote* is a named link from your local clone to a copy of the repository on GitHub. This clone has two:

| Remote | URL | Purpose |
|---|---|---|
| `origin` | github.com/psdillon/terraform-provider-cosmo | The fork. You push to this. |
| `upstream` | github.com/wundergraph/terraform-provider-cosmo | The original project. You only fetch from this. |

You can't push to upstream directly. Changes get there through a *pull request* (PR): you ask the
maintainers to merge a branch from your fork into their `main` branch, and they review it first.

### Branches

```
upstream/main ──●  (v0.5.3)
                 \
                  ● feat/organization-group   → PR 1 to upstream
                   \
                    ● feat/api-key            → PR 2 to upstream (needs PR 1)
                     │
                     release                  → what gets built and used
```

- **`main`**: kept identical to `upstream/main`. Never commit to it. It's just a clean starting point.
- **`feat/<name>`**: one branch per feature, and each one becomes one upstream PR. A feature branch
  starts from `upstream/main`, unless it depends on another feature: `feat/api-key` needs groups, so it
  starts from `feat/organization-group`.
- **`release`**: all feature branches combined, plus this file. Builds for yourself and others come from
  this branch. Right now it's `feat/api-key` plus this file.

Why split it up like this: upstream maintainers review small, focused PRs much faster than one big one.
`release` lets everyone use all the features together while those PRs are still in review.

### Adding a future feature

1. Update `main` from upstream: `git fetch upstream`, then `git checkout main` and `git merge --ff-only upstream/main`.
2. Create the branch from `main`: `git checkout -b feat/<name> main`.
3. Write the code, tests and examples. Run `make generate` to update the docs. Commit using
   [Conventional Commits](https://www.conventionalcommits.org/) (`feat: ...`, `fix: ...`).
4. Push it: `git push -u origin feat/<name>`.
5. Bring it into `release`: `git checkout release`, then `git merge feat/<name>`.
6. Publish a new fork release (see below), then open the upstream PR when the feature is ready.

## Getting the features to other developers

Terraform downloads *providers* (plugins such as this one) automatically. The `source` in a
`required_providers` block tells it where to get them. Upstream's address is `wundergraph/cosmo`, which
means "the `cosmo` provider published by `wundergraph` on the public Terraform Registry". The fork's
features aren't in that published provider, so other developers need another way to get it.

### Option A: publish the fork to the Terraform Registry (recommended)

The fork can be published to the public [Terraform Registry](https://registry.terraform.io) under its
own name, `psdillon/cosmo`. It's free. After that, other developers and CI pipelines don't need to
clone anything, install Go or edit config files. They only change one line in their Terraform code:

```hcl
terraform {
  required_providers {
    cosmo = {
      source  = "psdillon/cosmo"   # was "wundergraph/cosmo"
      version = "~> 0.6"
    }
  }
}
```

`terraform init` then downloads the right binary for each machine (Windows, macOS, Linux).

One-time setup (most of it is already in the repo, because upstream publishes the same way):

1. Create a GPG signing key. The registry checks the signature on every release. Add the private key
   and its passphrase to the fork's GitHub secrets as `GPG_PRIVATE_KEY` and `PASSPHRASE`.
2. Sign in to registry.terraform.io with GitHub, add the GPG public key, and publish the
   `psdillon/terraform-provider-cosmo` repository.
3. Run the **Release** GitHub Action (`.github/workflows/release.yml`) on the `release` branch. It tags
   a version, builds every platform with GoReleaser, signs the build and creates a GitHub release.
   The registry picks up the new release automatically.

Every later feature only needs step 3. Use version numbers above upstream's latest (v0.5.3) so it's
clear the fork is newer.

### Option B: build from source (works today, good for one or two people)

This is how the fork is used locally at the moment. Each developer needs Git and Go installed.

```powershell
git clone https://github.com/psdillon/terraform-provider-cosmo.git
cd terraform-provider-cosmo
git checkout release
go install .          # puts terraform-provider-cosmo.exe in $env:USERPROFILE\go\bin
```

Then create a Terraform CLI config file, such as `%APPDATA%\terraform.rc`. It tells Terraform to use the
local build instead of downloading `wundergraph/cosmo`:

```hcl
provider_installation {
  dev_overrides {
    "wundergraph/cosmo" = "C:/Users/<you>/go/bin"
  }
  direct {}
}
```

Point Terraform at the file with `$env:TF_CLI_CONFIG_FILE = "$env:APPDATA\terraform.rc"`. While a dev
override is active, Terraform prints a warning on each run and skips version checks. To update, run
`git pull` and then `go install .` again. This approach doesn't suit CI.

A middle ground is to download the zipped binary from a GitHub release on the fork (created by step 3
of Option A) and use the same `dev_overrides` setup. That avoids installing Go, but it's still manual.

### When upstream merges the features

Once upstream releases a version with these features, switch everyone back to `wundergraph/cosmo`.
Change the `source` line and update the existing state so Terraform knows the resources now belong to
the upstream provider:

```powershell
terraform state replace-provider psdillon/cosmo wundergraph/cosmo
terraform init -upgrade
```

## Current status

- [x] `cosmo_organization_group` resource and data source written, with unit tests passing
- [x] `cosmo_api_key` resource written, with unit tests passing
- [x] Data source confirmed against a real Cosmo org
- [ ] Manually test creating, updating, importing and destroying groups and API keys against a real org
- [ ] Run the acceptance tests (`TF_ACC=1`) against a real org
- [ ] Generate and commit docs separately on each feature branch (`make generate`)
- [ ] Set up publishing to the Terraform Registry as `psdillon/cosmo` and cut the first fork release
- [ ] Open PR 1 (`feat/organization-group` → `wundergraph/main`)
- [ ] After PR 1 merges, rebase `feat/api-key` onto the new `upstream/main` and open PR 2
- [ ] Follow-up PR: expose `target_id` on `cosmo_federated_graph` and `cosmo_subgraph`, so group rules can use `resources`

## Contributing upstream: the short version

1. Optionally open an issue on wundergraph/terraform-provider-cosmo first, describing the feature. That
   way the maintainers can say early on whether they want it and how.
2. On GitHub, open a PR from `psdillon:feat/organization-group` to `wundergraph:main`. Describe what it
   adds and how it was tested.
3. Respond to review comments by pushing more commits to the same branch. The PR updates automatically.
   Merge those changes into `release` too.
4. After it merges, update `main` from upstream, rebase the next feature branch onto it and open the
   next PR. Once upstream has released all the features, switch back to `wundergraph/cosmo` as
   described above.
