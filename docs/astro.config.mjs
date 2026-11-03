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
					label: 'Getting Started',
					items: [
						{ label: 'What is Axon?', slug: 'start/overview' },
						{ label: 'Quickstart', slug: 'start/quickstart' },
					],
				},
				{
					label: 'Core Concepts',
					items: [
						{ label: 'The Turn Loop', slug: 'concepts/turn-loop' },
						{ label: 'Context Management', slug: 'concepts/context' },
						{ label: 'Sessions & Memory', slug: 'concepts/sessions' },
						{ label: 'Events & UI', slug: 'concepts/events' },
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
					label: 'Configuration',
					items: [
						{ label: 'Agent Setup', slug: 'configuration/setup' },
						{ label: 'Runtime Policies', slug: 'configuration/yaml' },
						{ label: 'File Locations', slug: 'configuration/locations' },
					],
				},
				{
					label: 'Under the Hood',
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
