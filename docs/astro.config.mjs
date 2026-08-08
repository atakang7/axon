// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// https://astro.build/config
export default defineConfig({
	site: 'https://atakang7.github.io',
	base: '/axon',
	integrations: [
		starlight({
			title: 'Axon',
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/atakang7/axon' }],
			sidebar: [
				{
					label: 'Overview',
					items: [
						{ label: 'Introduction & Design', slug: 'overview/introduction' },
						{ label: 'Getting Started', slug: 'overview/getting-started' },
						{ label: 'Life of a Turn', slug: 'overview/life-of-a-turn' },
					],
				},
				{
					label: 'Guides',
					items: [
						{ label: 'Defining Tools', slug: 'guides/defining-tools' },
						{ label: 'Configuration & State', slug: 'guides/configuration-and-state' },
						{ label: 'Telemetry & Events', slug: 'guides/telemetry-and-events' },
						{ label: 'Cortex (Reference TUI)', slug: 'guides/cortex-reference' },
					],
				},
			],
		}),
	],
});
