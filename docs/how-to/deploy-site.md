# Deploy the documentation site

The `deploy-docs.yml` workflow deploys the site to GitHub Pages after a push to `main`.

1. From the repository root, check the site build:
   ```sh
   cd docs
   npm ci
   npm run build
   ```
2. Merge a pull request into `main`.
3. Open the [Deploy Docs to GitHub Pages](../../.github/workflows/deploy-docs.yml) workflow and inspect its build and deploy jobs.

The published site path is `https://atakang7.github.io/axon/`.
