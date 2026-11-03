# Why the site is separate

The runtime is a flat Go package at the repository root. The `docs/` directory is an independent Astro project with its own dependency lock.

Go builds and tests do not require Node.js. Starlight renders the API guides and project pages without adding a runtime dependency to the Go module. GitHub Actions builds the site from `docs/` and publishes `dist/` through the Pages workflow.
