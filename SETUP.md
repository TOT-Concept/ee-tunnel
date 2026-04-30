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

## 3. Generate the cosign keypair

In a clean local checkout of the **public** repo:

```sh
cosign generate-key-pair
# enter a strong passphrase when prompted
```

This creates `cosign.key` (private) and `cosign.pub` (public).

- Commit `cosign.pub` to the public repo. **Do not commit `cosign.key`.**
- Also copy `cosign.pub` to the monorepo at
  `frontend/public/ee-tunnel-cosign.pub` so `install.sh` can fetch it from
  `https://entityenricher.ai/ee-tunnel-cosign.pub`.

In the **public** repo's settings → Secrets → Actions:

- `COSIGN_PRIVATE_KEY` — paste the contents of `cosign.key` as-is.
- `COSIGN_PASSWORD` — paste the passphrase you chose.

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

## 6. First release

In the monorepo:

```sh
git tag ee-tunnel-v0.1.0
git push origin ee-tunnel-v0.1.0
```

The tag will be pushed to the public repo by the sync workflow, which triggers
`release.yml` there. The release builds 5 binaries, signs each with cosign,
and publishes a GitHub Release at:

```
https://github.com/TOT-Concept/ee-tunnel/releases/tag/ee-tunnel-v0.1.0
```

The install script at `https://entityenricher.ai/install.sh` will then pull
from `…/releases/latest/download/…` automatically — no app redeploy needed.

## Future improvement: Sigstore keyless signing

The current setup uses a long-lived cosign keypair. A future v1.1 should swap
to **Sigstore keyless signing** using GitHub OIDC. The release workflow signs
as the workflow identity
(`https://github.com/TOT-Concept/ee-tunnel/.github/workflows/release.yml@…`),
and `install.sh` verifies with:

```sh
cosign verify-blob \
  --certificate-identity-regexp "https://github.com/TOT-Concept/ee-tunnel/.github/workflows/release.yml@.*" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --signature "$DEST.sig" "$DEST"
```

No private key to rotate, no passphrase secret to leak.
