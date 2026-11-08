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

### Option B: download a release and override (works today)

The first attempts to publish to the Registry failed with timeouts. Until it's published, use this.
You don't need Go or a clone of the repo, and your Terraform code keeps
`source = "wundergraph/cosmo"`. A *dev override* is a setting in Terraform's own config file. It says
"when a config asks for `wundergraph/cosmo`, use the program in this folder instead of downloading it".

1. **Download the build for your machine** from
   [the v0.6.0 release](https://github.com/psdillon/terraform-provider-cosmo/releases/tag/v0.6.0):
   `windows_amd64` for most Windows PCs, `darwin_arm64` for Apple Silicon Macs, `linux_amd64` for Linux.
   Unzip it into a folder of its own. In Git Bash (or a macOS/Linux terminal):

   ```bash
   mkdir -p ~/terraform-providers/cosmo
   cd ~/terraform-providers/cosmo
   curl -LO https://github.com/psdillon/terraform-provider-cosmo/releases/download/v0.6.0/terraform-provider-cosmo_0.6.0_windows_amd64.zip
   unzip -o terraform-provider-cosmo_0.6.0_windows_amd64.zip
   ```

   The folder now contains `terraform-provider-cosmo_v0.6.0.exe` (no `.exe` on macOS or Linux).

2. **Create a Terraform CLI config file** that points `wundergraph/cosmo` at that folder. Use the
   folder's full path. On Windows, write it with forward slashes, for example
   `C:/Users/<you>/terraform-providers/cosmo`:

   ```bash
   cat > ~/.terraformrc <<EOF
   provider_installation {
     dev_overrides {
       "wundergraph/cosmo" = "$(cd ~/terraform-providers/cosmo && pwd -W 2>/dev/null || pwd)"
     }
     direct {}
   }
   EOF
   ```

3. **Tell Terraform to use that file.** Add this line to `~/.bashrc` so it applies to every new terminal:

   ```bash
   export TF_CLI_CONFIG_FILE=~/.terraformrc
   ```

4. **Check it.** In any Terraform project that uses the cosmo provider, with `COSMO_API_KEY` set, run
   `terraform plan`. It should start with *"Warning: Provider development overrides are in effect"*
   and name your folder. That warning appears on every run while the override is set, and it's
   expected. You don't need `terraform init` for the cosmo provider: the override replaces its
   download. Run `init` as usual if the project uses other providers too.

To update to a newer fork release, download its zip into the same folder and delete the old
`terraform-provider-cosmo_v*` file. To stop using the fork, remove the `dev_overrides` block.
Overrides suit people's own machines. For CI, wait for the Registry (Option A).

If you'd rather build from source, which needs Git and Go, run
`git clone https://github.com/psdillon/terraform-provider-cosmo.git`, then `git checkout release` and
`go install .`. Then point the override at the folder `go install` writes to (`$(go env GOPATH)/bin`).

### When upstream merges the features

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
- [ ] Groups resource tested manually against an org with RBAC enabled. Use
      `examples/resources/cosmo_organization_group`, then change the rules, import it by name and destroy it
- [ ] Run the acceptance tests (`TF_ACC=1`) against a real org
- [ ] Generate and commit docs separately on each feature branch (`make generate`)
- [x] Cut the first fork release: [v0.6.0](https://github.com/psdillon/terraform-provider-cosmo/releases/tag/v0.6.0), signed with key `B577DE22F0356A8A` (already added to the Registry)
- [ ] Publish to the Terraform Registry as `psdillon/cosmo`. Earlier attempts failed with 503/504 timeouts
      while the Registry fetched the release from GitHub. Retry **Publish → Provider** on registry.terraform.io
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
