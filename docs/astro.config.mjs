// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
	site: 'https://atakang7.github.io',
	base: '/axon',
	integrations: [
		starlight({
			title: 'Axon',
			description: 'A Go runtime for tool-using LLM agents.',
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/atakang7/axon' }],
			customCss: ['./src/styles/custom.css'],
			sidebar: [
				{
					label: 'Start',
					items: [
						{ label: 'Overview', slug: 'start/overview' },
						{ label: 'Quickstart', slug: 'start/quickstart' },
					],
				},
				{
					label: 'Configuration',
					items: [
						{ label: 'Config vs Settings', slug: 'configuration/surfaces' },
						{ label: 'axon.yaml Reference', slug: 'configuration/yaml' },
						{ label: 'File Locations', slug: 'configuration/locations' },
					],
				},
				{
					label: 'Tools',
					items: [
						{ label: 'Built-in Tools', slug: 'tools/builtins' },
						{ label: 'Custom Tools', slug: 'tools/custom' },
						{ label: 'MCP Servers', slug: 'tools/mcp' },
					],
				},
				{
					label: 'Runtime',
					items: [
						{ label: 'Turn Loop', slug: 'runtime/turn-loop' },
						{ label: 'Events', slug: 'runtime/events' },
						{ label: 'Sessions', slug: 'runtime/sessions' },
						{ label: 'Context Management', slug: 'runtime/context' },
					],
				},
				{
					label: 'Internals',
					items: [
						{ label: 'Architecture', slug: 'internals/architecture' },
						{ label: 'Security Boundaries', slug: 'internals/security' },
						{ label: 'Retry Logic', slug: 'internals/retries' },
					],
				},
			],
		}),
	],
});
