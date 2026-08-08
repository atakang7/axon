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
          label: 'Start here',
          items: [
            { label: 'What is Axon?', slug: 'overview/introduction' },
            { label: 'Getting started', slug: 'overview/getting-started' },
            { label: 'Architecture', slug: 'overview/architecture' },
            { label: 'Life of a turn', slug: 'overview/life-of-a-turn' },
          ],
        },
        {
          label: 'Core concepts',
          items: [
            { label: 'Sessions & state', slug: 'concepts/state-management' },
            { label: 'Context & pruning', slug: 'concepts/context-pruning' },
            { label: 'Tool execution', slug: 'concepts/tool-execution' },
            { label: 'Security model', slug: 'concepts/security-model' },
          ],
        },
        {
          label: 'Guides',
          items: [
            { label: 'Configuration & state locations', slug: 'guides/configuration-and-state' },
            { label: 'Models & providers', slug: 'guides/models-and-providers' },
            { label: 'Custom tools', slug: 'guides/defining-tools' },
            { label: 'MCP servers', slug: 'guides/mcp' },
            { label: 'Events & observability', slug: 'guides/telemetry-and-events' },
          ],
        },
        {
          label: 'Reference',
          items: [
            { label: 'Agent API', slug: 'reference/agent-api' },
            { label: 'Built-in tools', slug: 'reference/built-in-tools' },
            { label: 'Configuration', slug: 'reference/configuration' },
            { label: 'Events & errors', slug: 'reference/events-and-errors' },
          ],
        },
        {
          label: 'Internals',
          items: [
            { label: 'Source map', slug: 'internals/source-map' },
            { label: 'Runtime invariants', slug: 'internals/runtime-invariants' },
          ],
        },
      ],
    }),
  ],
});
