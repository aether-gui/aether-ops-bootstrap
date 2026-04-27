import type { SidebarsConfig } from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  defaultSidebar: [
    {
      type: 'category',
      label: 'Introduction',
      link: { type: 'doc', id: 'introduction/index' },
      items: [
        'introduction/concepts',
        'introduction/how-it-works',
        'introduction/the-two-artifacts',
        'introduction/glossary',
      ],
    },
    {
      type: 'category',
      label: 'Getting Started',
      link: { type: 'doc', id: 'getting-started/index' },
      items: [
        'getting-started/first-bootstrap',
        'getting-started/verifying',
        'getting-started/common-problems',
        'getting-started/next-steps',
      ],
    },
    {
      type: 'category',
      label: 'Build Guide',
      link: { type: 'doc', id: 'build-guide/index' },
      items: [
        'build-guide/bundle-yaml-reference',
        'build-guide/lockfile',
        'build-guide/manifest',
        'build-guide/building-locally',
        'build-guide/release-process',
        'build-guide/versioning',
        'build-guide/release-site',
      ],
    },
    {
      type: 'category',
      label: 'Bootstrap Guide',
      link: { type: 'doc', id: 'bootstrap-guide/index' },
      items: [
        'bootstrap-guide/cli-reference',
        'bootstrap-guide/components',
        'bootstrap-guide/state-file',
        'bootstrap-guide/on-disk-layout',
        'bootstrap-guide/upgrades-and-repair',
        'bootstrap-guide/troubleshooting',
        'bootstrap-guide/roadmap',
      ],
    },
  ],
};

export default sidebars;
