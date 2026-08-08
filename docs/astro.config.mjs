// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
  site: 'https://atakang7.github.io',
  base: '/axon',
  integrations: [
    starlight({
      title: 'Axon',
      social: [
        { icon: 'github', label: 'GitHub', href: 'https://github.com/atakang7/axon' },
      ],
      customCss: ['./src/styles/custom.css'],
      editLink: {
        baseUrl: 'https://github.com/atakang7/axon/edit/main/docs/',
      },
      sidebar: [
        {
          label: 'Get started',
          items: [
            { label: 'Overview', slug: 'start/overview' },
            { label: 'Quickstart', slug: 'start/quickstart' },
          ],
        },
        {
          label: 'Concepts',
          items: [
            { label: 'The runtime model', slug: 'concepts/runtime-model' },
            { label: 'Models & providers', slug: 'concepts/models-and-providers' },
            { label: 'Tools as capabilities', slug: 'concepts/tools-and-capabilities' },
            { label: 'Sessions & workspaces', slug: 'concepts/sessions-and-workspaces' },
            { label: 'Context lifecycle', slug: 'concepts/context-lifecycle' },
            { label: 'Background work', slug: 'concepts/background-work' },
            { label: 'Security model', slug: 'concepts/security-model' },
          ],
        },
        {
          label: 'How-to guides',
          items: [
            { label: 'Use axon.yaml', slug: 'guides/use-configuration-files' },
            { label: 'Use an OpenAI-compatible endpoint', slug: 'guides/openai-compatible-model' },
            { label: 'Implement a Model', slug: 'guides/custom-model' },
            { label: 'Add a custom tool', slug: 'guides/custom-tool' },
            { label: 'Replace a built-in tool', slug: 'guides/replace-built-in' },
            { label: 'Connect an MCP server', slug: 'guides/mcp' },
            { label: 'Run background processes', slug: 'guides/background-processes' },
            { label: 'Control a session', slug: 'guides/session-control' },
            { label: 'Build observability', slug: 'guides/observability' },
          ],
        },
        {
          label: 'Configuration',
          items: [
            { label: 'Configuration model', slug: 'configuration' },
            { label: 'Providers', slug: 'configuration/providers' },
            { label: 'Model requests', slug: 'configuration/model' },
            { label: 'Retry policy', slug: 'configuration/retry' },
            { label: 'Tool limits', slug: 'configuration/tools' },
            { label: 'Context & pruner', slug: 'configuration/pruner' },
            { label: 'Sessions & paths', slug: 'configuration/session' },
            { label: 'Environment variables', slug: 'configuration/environment' },
            { label: 'Durations & byte sizes', slug: 'configuration/scalars' },
            { label: 'Complete axon.yaml', slug: 'configuration/complete-example' },
          ],
        },
        {
          label: 'Reference',
          items: [
            { label: 'Agent', slug: 'reference/agent' },
            { label: 'Model & messages', slug: 'reference/model' },
            { label: 'Built-in tools', slug: 'reference/built-in-tools' },
            { label: 'Session', slug: 'reference/session' },
            { label: 'Events', slug: 'reference/events' },
            { label: 'Errors', slug: 'reference/errors' },
          ],
        },
        {
          label: 'Troubleshooting',
          items: [
            { label: 'Configuration', slug: 'troubleshooting/configuration' },
            { label: 'Runtime & tools', slug: 'troubleshooting/runtime' },
          ],
        },
        {
          label: 'Internals',
          items: [
            { label: 'Architecture', slug: 'internals/architecture' },
            { label: 'Source map', slug: 'internals/source-map' },
            { label: 'Runtime invariants', slug: 'internals/runtime-invariants' },
          ],
        },
      ],
    }),
  ],
});
