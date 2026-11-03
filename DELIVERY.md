# Delivery

The repository publishes GitHub releases for the Go library and separately deploys the documentation site. There is no application deployment manifest.

## 1. Pull request gate

```text
pull_request
    └── .github/workflows/ci.yml
        ├── Ubuntu: build → vet → gofmt → race tests
        ├── Windows: build → vet → tests
        ├── docs/: npm ci → Astro build
        └── Conventional Commits lint
```

`ci.yml` uses `go.mod` for Go and Node.js 22.12.0 for the site build. The commitlint job runs on pull requests.

## 2. Release path

```text
push to main / workflow_dispatch
    └── release.yml
        └── reusable ci.yml gate (must pass)
            └── semantic-release
                ├── calculate version from commits
                ├── tag v<version> and publish GitHub release
                └── generate GitHub release notes
```

`.releaserc.json` configures conventional-commit release rules and `main` as the release branch. `docs`, `fix` and `build` commits can trigger a patch; `feat` triggers a minor; breaking changes trigger a major. The workflow creates release notes through the GitHub API; it does not publish an application binary or container image.

## 3. Documentation deployment

```text
push to main / workflow_dispatch
    └── deploy-docs.yml
        └── npm ci → Astro build → docs/dist → GitHub Pages
```

Pages deployment has its own build and does not depend on the Go release job. `docs/astro.config.mjs` sets the site URL and `/axon` base path.

## 4. Failure boundary and verification

A failed reusable CI gate prevents `semantic-release` from running. A docs deployment failure is separate from release tagging. Check the relevant GitHub Actions run, the release's tag and notes, and the deployed Pages site before claiming a release succeeded.

The release workflow supports manual `workflow_dispatch` re-runs; there is no staging or promotion workflow in `.github/workflows/`.
