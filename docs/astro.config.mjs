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
						{ label: 'Introduction', slug: 'overview/introduction' },
						{ label: 'Quick Start', slug: 'overview/quick-start' },
					],
				},
				{
					label: 'Core Components',
					items: [
						{ label: 'Configuration', slug: 'core/configuration' },
						{ label: 'Tools', slug: 'core/tools' },
						{ label: 'Events & Observability', slug: 'core/events' },
					],
				},
			],
		}),
	],
});
