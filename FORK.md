# About this fork

This repository is a fork of [wundergraph/terraform-provider-cosmo](https://github.com/wundergraph/terraform-provider-cosmo),
the Terraform provider for WunderGraph Cosmo. It adds features that the original ("upstream") project
doesn't have yet. The plan is to contribute them back upstream. Until they're merged and released there,
the fork is published on the Terraform Registry as
[`psdillon/cosmo`](https://registry.terraform.io/providers/psdillon/cosmo/latest).

This file only exists in the fork, on the `release` branch. Keep it out of the branches used for pull
requests to upstream.

## Using the fork (start here)

The fork is a drop-in replacement for `wundergraph/cosmo`. It has all of upstream's resources plus the
ones listed under [What the fork adds](#what-the-fork-adds).

### 1. Point your Terraform code at the fork

In the `terraform` block of your project, change the provider's `source`:

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

Then run `terraform init -upgrade`. Terraform downloads the fork from the Registry, the same way it
downloads any provider. It reports the provider as "self-signed, key ID B577DE22F0356A8A", which is
normal for providers published by individuals.

### 2. If the project already has state from `wundergraph/cosmo`

Terraform's *state* records which provider manages each resource. If the project has already been
applied with `wundergraph/cosmo`, hand those resources over to the fork once, then re-run init:

```bash
terraform state replace-provider wundergraph/cosmo psdillon/cosmo
terraform init -upgrade
terraform plan   # should show no changes caused by the switch
```

If the state is stored remotely (for example in Azure Storage), this updates it there. Everyone using
the project then needs the `source` change from step 1.

### 3. Commit the lock file, with hashes for every platform

`terraform init` records the provider version and checksums in `.terraform.lock.hcl`. Commit that file.
If people use Windows while the pipeline runs on Linux, record checksums for both platforms. Otherwise
`init` on the other platform fails:

```bash
terraform providers lock -platform=windows_amd64 -platform=linux_amd64 -platform=darwin_arm64
```

### 4. Azure DevOps pipelines

No special setup is needed: `terraform init` in the pipeline downloads the fork from the Registry.
Store the Cosmo API key as a **secret** pipeline variable named `COSMO_API_KEY`. Azure DevOps doesn't
pass secrets to scripts unless you map them with `env:`:

```yaml
steps:
  - bash: |
      terraform init -input=false
      terraform plan -input=false -out=tfplan
    displayName: Terraform plan
    env:
      COSMO_API_KEY: $(COSMO_API_KEY)
```

The Terraform tasks from the Marketplace work the same way. Pass `COSMO_API_KEY` to them as an
environment variable.

### If you set up a dev override earlier

Earlier versions of this file described a `dev_overrides` workaround for use before the Registry
release. Remove the `dev_overrides` block from your Terraform CLI config file (`~/.terraformrc` or
`%APPDATA%\terraform.rc`) and the install step from your pipeline. Then follow the steps above.

## What the fork adds

| Feature | Type | Branch | Status |
|---|---|---|---|
| `cosmo_organization_group`: create and manage organization groups (role-based permissions) | resource + data source | `feat/organization-group` | Data source tested against a real org. **Resource not yet tested** (needs an org with RBAC) |
| `cosmo_api_key`: create organization API keys and assign them to a group | resource | `feat/api-key` | Tested against a real org: create, change group, import, destroy |

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

The full reference docs are on the
[Registry](https://registry.terraform.io/providers/psdillon/cosmo/latest/docs), and the examples are in
`examples/`.

### Help wanted: testing the groups resource

The groups resource still needs testing against an org with RBAC enabled. You'll need an
`organization-admin` API key for that org, set as `COSMO_API_KEY`. In an empty folder, create `main.tf`:

```hcl
terraform {
  required_providers {
    cosmo = {
      source  = "psdillon/cosmo"
      version = "~> 0.6"
    }
  }
}

resource "cosmo_namespace" "test" {
  name = "tf-test-groups"
}

resource "cosmo_organization_group" "test" {
  name        = "tf-test-group"
  description = "Created by Terraform test"
  rules = [
    { role = "organization-viewer" },
    { role = "namespace-admin", namespaces = [cosmo_namespace.test.id] },
  ]
}

resource "cosmo_api_key" "test" {
  name     = "tf-test-key"
  group_id = cosmo_organization_group.test.id
  expires  = "30_days"
}
```

It only creates new objects named `tf-test-…`, so it doesn't touch anything that already exists. Then:

1. `terraform init`, then `terraform apply`. Afterwards, `terraform plan` should say "No changes".
2. Change the `description`, and change the rules, for example by adding `{ role = "subgraph-publisher" }`.
   `terraform apply` should update the group **in place**, without destroying it. Then `terraform plan`
   should say "No changes".
3. `terraform state rm cosmo_organization_group.test`, then
   `terraform import cosmo_organization_group.test tf-test-group`. Afterwards, `terraform plan` should
   say "No changes".
4. `terraform destroy`. Check in Cosmo Studio that the group, the key and the namespace are gone.

Report anything unexpected as a GitHub issue on this fork, including the command output.

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
                     release                  → what gets built and published
```

- **`main`**: kept identical to `upstream/main`. Never commit to it. It's just a clean starting point.
- **`feat/<name>`**: one branch per feature, and each one becomes one upstream PR. A feature branch
  starts from `upstream/main`, unless it depends on another feature: `feat/api-key` needs groups, so it
  starts from `feat/organization-group`.
- **`release`**: all feature branches combined, plus this file and the fork's release settings. Every
  fork release is built from this branch.

Why split it up like this: upstream maintainers review small, focused PRs much faster than one big one.
`release` lets everyone use all the features together while those PRs are still in review.

### Adding a future feature

1. Update `main` from upstream: `git fetch upstream`, then `git checkout main` and `git merge --ff-only upstream/main`.
2. Create the branch from `main`: `git checkout -b feat/<name> main`.
3. Write the code, tests and examples. Run `make generate` to update the docs. On Windows, run
   `git add docs/` afterwards so the files that only changed line endings drop out. Commit using
   [Conventional Commits](https://www.conventionalcommits.org/) (`feat: ...`, `fix: ...`).
4. Push it: `git push -u origin feat/<name>`.
5. Bring it into `release`: `git checkout release`, then `git merge feat/<name>`, then `git push`.
6. Publish a new fork release (below), then open the upstream PR when the feature is ready.

For local development, `go install .` builds the provider, and a `dev_overrides` block in the Terraform
CLI config file points Terraform at that build instead of the Registry.

## Publishing a fork release

Run the **Release** workflow on the `release` branch:

```bash
gh workflow run release.yml -R psdillon/terraform-provider-cosmo --ref release
```

or open GitHub → **Actions** → **Release** → **Run workflow** and pick `release`. It takes about 10 minutes.
The workflow:

1. Picks the next version from the commit messages since the last tag. `feat:` bumps the minor version
   (0.6.0 → 0.7.0), and anything else bumps the patch version (0.6.0 → 0.6.1). It then tags the commit.
2. Builds every platform with GoReleaser, signs the checksums with the fork's GPG key and creates a
   GitHub release.
3. If anything fails, deletes the tag again so the run can simply be retried.

The Registry picks up the new GitHub release automatically within a few minutes.

One-time setup, already done:

- GPG signing key `B577DE22F0356A8A`: the private key and passphrase are stored in the fork's GitHub
  secrets (`GPG_PRIVATE_KEY`, `PASSPHRASE`), and the public key is added to the Registry under
  `psdillon`. If the key is ever lost, create a new one, update both secrets and add the new public key
  to the Registry.
- The fork's `release.yml` differs from upstream's in two ways. It releases from `release` instead of
  `main`, and it installs GoReleaser through the Go proxy, because downloading it from GitHub kept failing.

## When upstream merges the features

Once upstream releases a version with these features, switch everyone back to `wundergraph/cosmo`.
Change the `source` line and update the existing state so Terraform knows the resources now belong to
the upstream provider:

```bash
terraform state replace-provider psdillon/cosmo wundergraph/cosmo
terraform init -upgrade
```

## Current status

- [x] `cosmo_organization_group` resource and data source written, with unit tests passing
- [x] `cosmo_api_key` resource written, with unit tests passing
- [x] Data source confirmed against a real Cosmo org
- [x] API keys tested manually against a real org: create, change group, import, destroy
- [x] First fork release, [v0.6.0](https://github.com/psdillon/terraform-provider-cosmo/releases/tag/v0.6.0), published to the
      [Terraform Registry](https://registry.terraform.io/providers/psdillon/cosmo/latest) as `psdillon/cosmo`
- [ ] Groups resource tested manually against an org with RBAC enabled (see
      [Help wanted](#help-wanted-testing-the-groups-resource))
- [ ] Run the acceptance tests (`TF_ACC=1`) against a real org
- [ ] Generate and commit docs separately on each feature branch (`make generate`)
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
