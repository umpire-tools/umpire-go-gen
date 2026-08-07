import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'Umpire Spec',
  description: 'Cross-language conformance specification for Umpire — JSON Schema + fixtures.',
  base: '/',
  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/favicon.svg' }],
  ],
  themeConfig: {
    appearance: 'dark',
    nav: [
      { text: 'Schema Reference', link: '/schema' },
      { text: 'Integrating', link: '/integrating' },
      { text: 'GitHub', link: 'https://github.com/umpire-tools/umpire-spec' },
    ],
    sidebar: [
      {
        text: 'Spec',
        items: [
          { text: 'Schema Reference', link: '/schema' },
          { text: 'Integrating a Port', link: '/integrating' },
        ],
      },
    ],
    socialLinks: [
      { icon: 'github', link: 'https://github.com/umpire-tools/umpire-spec' },
    ],
    editLink: {
      pattern: 'https://github.com/umpire-tools/umpire-spec/edit/main/docs/:path',
      text: 'Edit this page',
    },
  },
})
