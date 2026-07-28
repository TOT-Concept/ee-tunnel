# Public Repository Setup (one-time)

This connector is maintained as a git **subtree** of the main `EntityEnricher`
monorepo and synced to a public sibling repo at
[`TOT-Concept/ee-tunnel`](https://github.com/TOT-Concept/ee-tunnel) via
[`.github/workflows/ee-tunnel-sync.yml`](../../.github/workflows/ee-tunnel-sync.yml)
(in the monorepo root) on every push to `main`.

The subtree push only works once the public repo exists. The steps below are a
one-time bootstrap; after this, releases are driven entirely by tagging.

## 1. Create the public repository

In the `TOT-Concept` GitHub org:

- Repo name: `ee-tunnel`
- Visibility: **Public**
- Description:
  *"Open-source CLI to connect a local Ollama (or other tools) to your Entity
  Enricher organization. Maintained as a subtree of the main monorepo."*
- Topics: `ollama` `cli` `tunnel` `websocket` `entity-enricher` `golang`
- Do **not** initialize with a README — the first subtree push will populate it.

## 2. Configure the EE_TUNNEL_PUBLIC_REPO_TOKEN secret

The sync workflow pushes via a fine-grained PAT scoped to the public repo:

1. Generate a fine-grained PAT (`https://github.com/settings/personal-access-tokens/new`)
   with **Contents: write** on `TOT-Concept/ee-tunnel` only.
2. In the **monorepo's** repo settings → Secrets → Actions, add a secret named
   `EE_TUNNEL_PUBLIC_REPO_TOKEN` containing that PAT.

## 3. Release signing — Sigstore keyless (no setup needed)

Releases are signed with **Sigstore keyless signing**: `release.yml` obtains a
short-lived certificate from Fulcio bound to its GitHub OIDC workflow identity
(`https://github.com/TOT-Concept/ee-tunnel/.github/workflows/release.yml@refs/tags/ee-tunnel-v*`),
signs each binary, and the signature is logged in the public
[Rekor](https://search.sigstore.dev) transparency log. Each release asset ships
with a `.sig` + `.pem` pair, and `install.sh` / `install.ps1` verify with:

```sh
cosign verify-blob \
  --certificate "$ASSET.pem" \
  --certificate-identity-regexp "^https://github\.com/TOT-Concept/ee-tunnel/\.github/workflows/release\.yml@refs/tags/ee-tunnel-v" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --signature "$ASSET.sig" "$ASSET"
```

There is **no long-lived signing key**: nothing to generate, store, rotate, or
leak. The trust anchors are the workflow identity above and GitHub's OIDC
issuer — which makes branch/tag protection (§4) the effective security
perimeter of the release pipeline. Any signature minted in this repo's name is
publicly auditable in Rekor.

> **Migration note (2026-07):** the original setup used a long-lived cosign
> keypair (`COSIGN_PRIVATE_KEY` / `COSIGN_PASSWORD` secrets in this repo,
> pubkey served from `entityenricher.ai`). No release was ever signed with it.
> If those secrets still exist, delete them
> (`gh secret delete COSIGN_PRIVATE_KEY -R TOT-Concept/ee-tunnel`, same for
> `COSIGN_PASSWORD`) and destroy any local `cosign.key` backup.

## 4. Branch protection on the public repo

Settings → Branches → `main`:

- Require a pull request before merging
- Require at least 1 approving review
- Require status checks to pass: `Vet + build + test`, `golangci-lint`

## 5. First sync

The very first sync is bootstrapped by triggering the workflow manually:

```
gh workflow run "Sync ee-tunnel"
```

(or via the **Run workflow** button in the Actions tab of the monorepo).

## 6. Releases

In the monorepo:

```sh
git tag ee-tunnel-v0.1.0
git push origin ee-tunnel-v0.1.0
```

The tag push triggers
[`ee-tunnel-release-tag.yml`](../../.github/workflows/ee-tunnel-release-tag.yml)
(monorepo root), which recomputes the deterministic subtree split of the tagged
commit and pushes the tag onto the corresponding split commit in the public
repo. (The regular sync workflow only pushes the branch — never tags.) That tag
triggers `release.yml` there, which builds 5 binaries, signs each keyless (§3),
and publishes a GitHub Release at:

```
https://github.com/TOT-Concept/ee-tunnel/releases/tag/ee-tunnel-v0.1.0
```

The install script (canonically
`https://raw.githubusercontent.com/TOT-Concept/ee-tunnel/main/install.sh`,
aliased as `https://entityenricher.ai/install.sh` via a backend 302) pulls from
`…/releases/latest/download/…` automatically — no app redeploy needed.
