# aether-ops-bootstrap documentation site

This directory contains the [Docusaurus](https://docusaurus.io) site published to
<https://aether-gui.github.io/aether-ops-bootstrap/>.

## Local development

```bash
npm install
npm start         # http://localhost:3000
```

## Production build

```bash
npm run build     # output in ./build
npm run serve     # preview the production build locally
```

The build is configured with `onBrokenLinks: 'throw'`, so any broken internal
link will fail the build. CI runs `npm run build` on every PR that touches
`website/`.

## Known limitations in 0.1.x

- **Mermaid diagrams are disabled.** `@docusaurus/theme-mermaid` has an
  SSR / ColorModeProvider regression across the 3.7 – 3.10 line that crashes
  static site generation on pages containing `mermaid` fenced blocks. Those
  blocks currently render as plain code blocks showing the Mermaid source.
  Re-enable by adding `@docusaurus/theme-mermaid` back to `package.json` and
  restoring the `markdown.mermaid` / `themes` / `themeConfig.mermaid` entries
  in `docusaurus.config.ts` once upstream ships a fix.

## Publishing

The site is deployed by `.github/workflows/docs.yml` on:

- every push to `main` that touches `website/`
- every `v*` tag push

No manual `docusaurus deploy` is needed.
