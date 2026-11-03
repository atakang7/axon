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
					label: 'How to',
					items: [
						{ label: 'Run an agent', slug: 'how-to/first-agent' },
						{ label: 'Add a tool', slug: 'how-to/add-tool' },
						{ label: 'Connect an MCP server', slug: 'how-to/connect-mcp' },
					],
				},
				{
					label: 'Reference',
					items: [
						{ label: 'Go API', slug: 'reference/api' },
						{ label: 'Settings', slug: 'reference/settings' },
						{ label: 'Built-in tools', slug: 'reference/tools' },
						{ label: 'Events', slug: 'reference/events' },
					],
				},
				{
					label: 'Explanation',
					items: [
						{ label: 'Architecture', slug: 'explanation/architecture' },
						{ label: 'Context checkpoints', slug: 'explanation/context' },
						{ label: 'Retry policy', slug: 'explanation/retries' },
						{ label: 'Tool boundaries', slug: 'explanation/security' },
					],
				},
			],
		}),
	],
});
