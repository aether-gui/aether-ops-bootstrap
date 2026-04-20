import { themes as prismThemes } from 'prism-react-renderer';
import type { Config } from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const githubOrg = 'aether-gui';
const githubRepo = 'aether-ops-bootstrap';
const editBase = `https://github.com/${githubOrg}/${githubRepo}/tree/main/website/`;

const config: Config = {
  title: 'aether-ops-bootstrap',
  tagline: 'Offline bootstrap for the aether-ops management plane',
  favicon: 'img/favicon.ico',

  url: `https://${githubOrg}.github.io`,
  baseUrl: `/${githubRepo}/`,

  organizationName: githubOrg,
  projectName: githubRepo,
  trailingSlash: false,

  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'throw',

  // Mermaid diagrams are intentionally omitted in 0.1.x due to a
  // @docusaurus/theme-mermaid SSR / ColorModeProvider regression. Diagrams
  // in the source markdown render as labelled code blocks until the issue
  // is fixed upstream.

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          routeBasePath: '/',
          sidebarPath: './sidebars.ts',
          editUrl: editBase,
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  plugins: [
    [
      require.resolve('@easyops-cn/docusaurus-search-local'),
      {
        hashed: true,
        indexDocs: true,
        indexBlog: false,
        indexPages: true,
        docsRouteBasePath: '/',
      },
    ],
  ],

  themeConfig: {
    colorMode: {
      defaultMode: 'dark',
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'aether-ops-bootstrap',
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'defaultSidebar',
          position: 'left',
          label: 'Docs',
        },
        {
          label: 'v0.1.x Alpha',
          position: 'right',
          href: `https://github.com/${githubOrg}/${githubRepo}/releases`,
          className: 'navbar-version-badge',
        },
        {
          href: `https://github.com/${githubOrg}/${githubRepo}`,
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            { label: 'Introduction', to: '/introduction' },
            { label: 'Getting Started', to: '/getting-started' },
            { label: 'Build Guide', to: '/build-guide' },
            { label: 'Bootstrap Guide', to: '/bootstrap-guide' },
          ],
        },
        {
          title: 'Project',
          items: [
            {
              label: 'GitHub',
              href: `https://github.com/${githubOrg}/${githubRepo}`,
            },
            {
              label: 'Releases',
              href: `https://github.com/${githubOrg}/${githubRepo}/releases`,
            },
            {
              label: 'Design doc',
              href: `https://github.com/${githubOrg}/${githubRepo}/blob/main/DESIGN.md`,
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} aether-gui.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'yaml', 'json', 'go', 'toml', 'ini'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
