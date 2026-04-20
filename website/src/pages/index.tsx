import React from 'react';
import Link from '@docusaurus/Link';
import Layout from '@theme/Layout';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';

export default function Home(): JSX.Element {
  const { siteConfig } = useDocusaurusContext();
  return (
    <Layout
      title={siteConfig.title}
      description={siteConfig.tagline}
    >
      <header
        style={{
          padding: '4rem 1rem 3rem',
          textAlign: 'center',
          background:
            'linear-gradient(135deg, var(--ifm-color-primary-darkest), var(--ifm-color-primary))',
          color: '#fff',
        }}
      >
        <h1 style={{ fontSize: '2.6rem', margin: 0 }}>{siteConfig.title}</h1>
        <p style={{ fontSize: '1.2rem', marginTop: '1rem', opacity: 0.9 }}>
          {siteConfig.tagline}
        </p>
        <p style={{ opacity: 0.8, maxWidth: 720, margin: '1rem auto' }}>
          A statically linked launcher and an offline bundle that together turn a
          freshly installed Ubuntu Server into a running aether-ops management
          plane on RKE2 — without ever touching the network.
        </p>
        <div style={{ marginTop: '2rem', display: 'flex', gap: '0.75rem', justifyContent: 'center', flexWrap: 'wrap' }}>
          <Link
            className="button button--secondary button--lg"
            to="/introduction"
          >
            What is this?
          </Link>
          <Link
            className="button button--primary button--lg"
            to="/getting-started"
          >
            Bootstrap a node →
          </Link>
        </div>
        <p style={{ marginTop: '1.5rem', fontSize: '0.85rem', opacity: 0.75 }}>
          Documentation for v0.1.x Alpha
        </p>
      </header>

      <main style={{ maxWidth: 960, margin: '0 auto', padding: '3rem 1rem' }}>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))',
            gap: '1.5rem',
          }}
        >
          <Card
            title="Introduction"
            body="What the project is, why it exists, and how the pieces fit together."
            to="/introduction"
          />
          <Card
            title="Getting Started"
            body="Bootstrap a fresh Ubuntu host end-to-end using a launcher binary and a bundle tarball."
            to="/getting-started"
          />
          <Card
            title="Build Guide"
            body="Produce bundles from bundle.yaml, understand the lockfile and manifest, and cut releases."
            to="/build-guide"
          />
          <Card
            title="Bootstrap Guide"
            body="Reference for the launcher: commands, components, state, upgrades, troubleshooting."
            to="/bootstrap-guide"
          />
        </div>
      </main>
    </Layout>
  );
}

function Card({ title, body, to }: { title: string; body: string; to: string }) {
  return (
    <Link
      to={to}
      style={{
        display: 'block',
        padding: '1.5rem',
        border: '1px solid var(--ifm-color-emphasis-200)',
        borderRadius: 8,
        textDecoration: 'none',
        color: 'inherit',
        background: 'var(--ifm-background-surface-color)',
      }}
    >
      <h3 style={{ marginTop: 0 }}>{title}</h3>
      <p style={{ margin: 0, color: 'var(--ifm-color-emphasis-700)' }}>{body}</p>
    </Link>
  );
}
