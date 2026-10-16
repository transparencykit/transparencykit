# Brand guidelines

TransparencyKit's identity is **Open Ledger**: a document outline with one line extending
through an opening. The frame represents the retained record; the extending line represents
a public disclosure drawn from it. Use the identity to make the distinction between private
case records and verifiable public files clear.

![TransparencyKit Open Ledger logo.](../tools/site/src/assets/brand/logo-light.svg)

## Name and voice

Write **TransparencyKit** in product copy and page titles. Use `transparencykit` for the
binary, module paths and configuration filenames, and `tkit` for the short CLI name.

Use direct, specific language: record, publish, sign, verify. Explain what someone can check
before describing the cryptography. Describe the product as **tamper-evident**. A signature
authenticates published files; it does not prove that every event was recorded or that the
reported calculations are correct. Hash links need retained trusted copies or external
checkpoints to expose rollback. Do not present the logo, a checkmark or a static badge as a
live verification result.

## Logo and symbol

Use the supplied SVGs as the canonical artwork. The wordmark has outlined letterforms;
viewers do not need a font installed to reproduce it. The website's text uses Spectral, while
the approved wordmark is outlined from Georgia. Do not retype or redraw the wordmark.

![Open Ledger symbol: a document outline and a line extending through its open edge.](../tools/site/src/assets/brand/mark-light.svg)

| Asset | Use |
|---|---|
| [Primary logo](../tools/site/src/assets/brand/logo-light.svg) | Logo on white or light paper |
| [Reversed logo](../tools/site/src/assets/brand/logo-dark.svg) | Logo on the dark teal background |
| [Primary symbol](../tools/site/src/assets/brand/mark-light.svg) | Compact navigation, diagrams and product illustrations |
| [Reversed symbol](../tools/site/src/assets/brand/mark-dark.svg) | Symbol on dark backgrounds |
| [One-color symbol](../tools/site/src/assets/brand/mark-monochrome.svg) | Single-ink print or engraving; use a solid contrasting color |

Keep at least **12 units of clear space** around the symbol on its 64-unit drawing grid:
twice the six-unit line thickness. Keep that same clearance around a complete logo lockup.
Use the full logo at **200 CSS pixels or wider**, and the symbol at **16 pixels or larger**.
Use the specially adjusted favicon artwork for a 16-pixel browser icon.

Keep the proportions, opening, line lengths and spacing intact. Use the light or reversed
asset that matches the background. Do not stretch, rotate, add a shadow, add a gradient,
close the opening, or place the mark over a busy image. Do not add a shield or checkmark to
the logo: its job is to identify the product, not to certify a publication.

## Favicon and app icons

The favicon uses a dark teal tile with paper-colored geometry and a mint disclosure line.
The tile works in light and dark browser chrome. The 16-pixel artwork adjusts the geometry
for the small raster grid; the touch icon adds more surrounding space.

![TransparencyKit favicon at 16 pixels.](../tools/site/src/favicon-16.png)
![TransparencyKit favicon at 32 pixels.](../tools/site/src/favicon-32.png)

| Asset | Size or format |
|---|---|
| [SVG favicon](../tools/site/src/favicon.svg) | Scalable browser icon |
| [ICO favicon](../tools/site/src/favicon.ico) | 16, 32 and 48 pixel images in one file |
| [Small SVG source](../tools/site/src/assets/brand/favicon-small.svg) | Drawing adjusted for 16 pixels |
| [Apple touch icon](../tools/site/src/apple-touch-icon.png) | 180 × 180 pixels |
| [App icon](../tools/site/src/assets/brand/app-icon-512.png) | 512 × 512 pixels |

## Color

The palette continues the website's paper-and-ink appearance. Dark teal is the identity color;
green draws attention to one or two focal elements. Mint is the accent on dark backgrounds.
Amber is reserved for third-country facts or a contextual caution, not routine decoration.

| Role | Light theme | Dark theme |
|---|---|---|
| Paper / background | `#f5f7f6` | `#0d1f22` |
| Ink / primary text | `#0f2a2e` | `#e4eeeb` |
| Secondary text | `#4b6266` | `#9fb5b1` |
| Glass / tinted surface | `#dcebe7` | `#16343a` |
| Seal / focal accent | `#1e6b57` | `#5fc4a5` |
| Decorative rule | `#c9d8d4` | `#24464c` |
| Amber / contextual caution | `#a8620f` | `#e3a04f` |

Illustrations use white (`#ffffff`) cards on light paper and `#12292d` cards on dark paper.
Their essential borders use secondary-text colors; decorative rules may be lighter. The
dark illustration rule is `#395559`. Do not use mint as small text on light paper.

Maintain at least 4.5:1 contrast for normal text and 3:1 for essential graphical marks against
their actual backgrounds. Do not use color as the only way to communicate a distinction:
label private boundaries and draw publication links with an explicit direction.

## Typography

| Surface | Family | Use |
|---|---|---|
| Website headings and prose | Spectral, with Georgia and serif fallbacks | An editorial, readable document voice |
| Website commands and metadata | IBM Plex Mono, with system monospace fallbacks | Commands, hashes and identifiers |
| Standalone illustration titles | Georgia, then Times New Roman and serif | Portable editorial titles |
| Standalone illustration labels | Arial, then Helvetica and sans-serif | Human-readable component names |
| Standalone technical labels | Menlo, then Courier New and monospace | Hashes, filenames and technical notes |

The website self-hosts Spectral and IBM Plex Mono from its locked `@fontsource` packages.
Standalone SVG illustrations use local font stacks and make no font requests. Do not add
Google Fonts, remote stylesheets, analytics, or externally hosted image assets to the site.
Keep the outlined wordmark separate from ordinary text styling.

## Illustrations and diagrams

Use a small number of components, a clear reading order and one or two focal accents. Draw
orthogonal connectors with visible arrowheads, hairline rules and restrained corner radii.
Prefer a clean background to a decorative texture. Put technical detail in captions or the
surrounding documentation when it would overwhelm the main idea.

| Illustration | Placement and meaning |
|---|---|
| [Disclosure illustration](../tools/site/src/assets/illustrations/hero-light.svg) | Landing-page introduction: the case file stays private while a separate signed statement becomes public |
| [Concept overview](../tools/site/src/assets/illustrations/concept-light.svg) | Desktop explanation of the ledger, publisher, public files and independent verifier |
| [Compact concept](../tools/site/src/assets/illustrations/concept-compact-light.svg) | Documentation and narrow screens, with a vertical reading order |
| [Publication links](../tools/site/src/assets/illustrations/history-light.svg) | Desktop explanation of corrections and backward manifest links |
| [Compact publication links](../tools/site/src/assets/illustrations/history-compact-light.svg) | Documentation and narrow screens |

Each `-light.svg` has a matching `-dark.svg`. Use a `<picture>` source selected with
`prefers-color-scheme: dark`. On the landing page, switch the concept and history diagrams
to their compact versions at 1024 CSS pixels and below. Compact figures are at most 384 CSS
pixels wide so their labels stay readable. Do not shrink the wide diagrams to phone width.

SVGs must contain a descriptive `<title>` and `<desc>` with uniquely prefixed IDs, and an
`aria-labelledby` reference. An HTML `<img>` still needs useful alternative text. Captions
should explain the point rather than repeat each shape. Keep illustration hashes obviously
illustrative, and retain the notes about trusted public keys, exact DSA data and rollback.

The editorial diagram approach was informed by
[Cathryn Lavery's Diagram Design](https://github.com/cathrynlavery/diagram-design), adapted to
TransparencyKit's palette, system-font SVGs and offline website requirements.

## Asset maintenance

Approved artwork lives in `tools/site/src/assets/brand/` and
`tools/site/src/assets/illustrations/`; browser icons live directly in `tools/site/src/`.
Those SVGs are the editable production sources. The earlier `tools/site/design-proposals/`
folder is a design study, not a build dependency or the source of future production changes.

Documentation lives in `docs/*.md`. Reference artwork using paths such as
`../tools/site/src/assets/illustrations/concept-compact-light.svg`: these work in the
repository, and the website builder rewrites them to local published assets and selects the
dark variant. Add new documentation pages to `DOCS` in `tools/site/build.mjs`.

After editing artwork, check light and dark themes, 16-pixel icon legibility, narrow layouts,
alternative text and local asset links. Build the website with `npm ci && npm run build` in
`tools/site/`. Inspect the rendered pages before shipping. Branding changes do not alter the
ledger format, signed report schemas or the operator's publication data.
