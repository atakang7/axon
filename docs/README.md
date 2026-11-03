Part of [axon](../README.md)

# Documentation site

Starlight builds the Axon documentation served from GitHub Pages.

## Run

```sh
cd docs
npm ci
npm run dev
```

## Test

```sh
npm run build
```

## Config

| Setting | Default | What |
| --- | --- | --- |
| Node.js | 22.12.0 in CI | Site build runtime |
| `site` | `https://atakang7.github.io` | Canonical host in `astro.config.mjs` |
| `base` | `/axon` | Site path |

## Interfaces

| Path | What |
| --- | --- |
| `src/content/docs/` | Starlight Markdown pages |
| `src/components/` | Landing page components |
| `public/` | Static assets |
| `dist/` | Generated site, ignored by Git |
| `.github/workflows/deploy-docs.yml` | Pages deployment workflow at repository root |

## Docs

- [Run an agent](src/content/docs/how-to/first-agent.md)
- [Add a tool](src/content/docs/how-to/add-tool.md)
- [Connect an MCP server](src/content/docs/how-to/connect-mcp.md)
- [Go API](src/content/docs/reference/api.md)
- [Settings](src/content/docs/reference/settings.md)
- [Tools](src/content/docs/reference/tools.md)
- [Events](src/content/docs/reference/events.md)
- [Runtime architecture](src/content/docs/explanation/architecture.md)
- [Context checkpoints](src/content/docs/explanation/context.md)
- [Retry policy](src/content/docs/explanation/retries.md)
- [Tool boundaries](src/content/docs/explanation/security.md)
- [Deploy the site](how-to/deploy-site.md)
- [Site paths](reference/site-files.md)
- [Site layout](explanation/site-layout.md)
