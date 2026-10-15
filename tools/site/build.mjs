// Builds the TransparencyKit website into ../../_site (or $SITE_OUT).
//   - src/index.html, style.css, favicon.svg  -> site root
//   - docs/*.md (repository root)             -> docs/<name>.html, rendered with marked
//   - fonts from @fontsource                  -> fonts/ (self-hosted; no third-party requests)
// All links are relative, so the site works on github.io/<repo>/ and on a custom domain.
import { Marked, Renderer } from "marked";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "../..");
const out = path.resolve(process.env.SITE_OUT || path.join(repo, "_site"));
const REPO_URL = "https://github.com/transparencykit/transparencykit";

// Documentation pages in navigation order. Files not listed here are not published.
const DOCS = [
  { file: "index.md", title: "Overview" },
  { file: "getting-started.md", title: "Getting started" },
  { file: "cli.md", title: "CLI reference" },
  { file: "storage.md", title: "Storage requirements" },
  { file: "verifying.md", title: "Verifying a publication" },
  { file: "OPERATIONS.md", title: "Operations" },
  { file: "SPEC.md", title: "Specification" },
  { file: "TESTING.md", title: "Testing" },
];

const escapeHTML = (s) =>
  s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

function slugify(text) {
  return text
    .toLowerCase()
    .replace(/<[^>]+>/g, "")
    .replace(/&[a-z]+;/g, "")
    .replace(/[^\p{L}\p{N}\s-]/gu, "")
    .trim()
    .replace(/\s+/g, "-");
}

const docFiles = new Set(DOCS.map((d) => d.file));

// Rewrite links between Markdown files so they work on the site.
function rewriteHref(href) {
  if (!href || /^(https?:|mailto:|#)/.test(href)) return href;
  const [p, hash = ""] = href.split("#");
  const frag = hash ? "#" + hash : "";
  const base = path.posix.basename(p);
  if (p.endsWith(".md") && docFiles.has(base) && !p.includes("/")) {
    return base.replace(/\.md$/, ".html") + frag;
  }
  // Anything else in the repository (README, LICENSE, source files): link to GitHub.
  const target = path.posix.normalize(path.posix.join("docs", p));
  return `${REPO_URL}/blob/main/${target}${frag}`;
}

function render(md) {
  const headings = [];
  const used = new Map();
  const renderer = {
    heading({ tokens, depth }) {
      const html = this.parser.parseInline(tokens);
      let id = slugify(html) || "section";
      const n = used.get(id) || 0;
      used.set(id, n + 1);
      if (n) id += "-" + n;
      if (depth === 2) headings.push({ id, html });
      const anchor = depth > 1 && depth < 4 ? ` <a class="anchor" href="#${id}" aria-label="Link to this section">#</a>` : "";
      return `<h${depth} id="${id}">${html}${anchor}</h${depth}>\n`;
    },
    link({ href, title, tokens }) {
      const text = this.parser.parseInline(tokens);
      const t = title ? ` title="${escapeHTML(title)}"` : "";
      return `<a href="${escapeHTML(rewriteHref(href))}"${t}>${text}</a>`;
    },
    table(token) {
      return `<div class="table-wrap">${Renderer.prototype.table.call(this, token)}</div>\n`;
    },
  };
  const m = new Marked({ gfm: true });
  m.use({ renderer });
  const html = m.parse(md);
  const h1 = (md.match(/^#\s+(.+)$/m) || [])[1];
  return { html, headings, h1 };
}

function docPage({ title, file, html, headings, description }) {
  const outName = file.replace(/\.md$/, ".html");
  const nav = DOCS.map((d) => {
    const href = d.file.replace(/\.md$/, ".html");
    const current = d.file === file;
    const sub = current && headings.length > 1
      ? `<ol>${headings.map((h) => `<li><a href="#${h.id}">${h.html}</a></li>`).join("")}</ol>`
      : "";
    return `<li><a href="${href}"${current ? ' aria-current="page"' : ""}>${escapeHTML(d.title)}</a>${sub}</li>`;
  }).join("\n");
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${escapeHTML(title)}: TransparencyKit documentation</title>
<meta name="description" content="${escapeHTML(description)}">
<link rel="icon" href="../favicon.svg" type="image/svg+xml">
<link rel="stylesheet" href="../style.css">
</head>
<body class="docs">
<a class="skip" href="#content">Skip to content</a>
<header class="masthead">
  <a class="wordmark" href="../">TransparencyKit <span>documentation</span></a>
  <nav aria-label="Site">
    <a href="./"${outName === "index.html" ? ' aria-current="page"' : ""}>Documentation</a>
    <a href="verifying.html">Verify a report</a>
    <a href="${REPO_URL}">GitHub</a>
  </nav>
</header>
<div class="docs-shell">
  <nav class="docs-nav" aria-label="Documentation">
    <ol>
${nav}
    </ol>
    <p class="source"><a href="${REPO_URL}/blob/main/docs/${file}">Edit this page</a></p>
  </nav>
  <article class="prose" id="content">
${html}
  </article>
</div>
<footer class="site-footer">
  <span>TransparencyKit is open source under the Apache-2.0 licence.</span>
  <span><a href="../">Home</a> &nbsp; <a href="${REPO_URL}">GitHub</a></span>
</footer>
</body>
</html>
`;
}

function firstParagraph(md) {
  const p = md.split(/\n\s*\n/).find((b) => b.trim() && !b.startsWith("#") && !b.startsWith("|") && !b.startsWith("```") && !b.startsWith(">"));
  return (p || "").replace(/\s+/g, " ").replace(/[*`_[\]]/g, "").replace(/\(([^)]*)\)/g, "").slice(0, 200).trim();
}

// --- build ------------------------------------------------------------------
fs.rmSync(out, { recursive: true, force: true });
fs.mkdirSync(path.join(out, "docs"), { recursive: true });
fs.mkdirSync(path.join(out, "fonts"), { recursive: true });

for (const f of ["index.html", "style.css", "favicon.svg"]) {
  fs.copyFileSync(path.join(here, "src", f), path.join(out, f));
}

const fonts = {
  "spectral-400.woff2": "@fontsource/spectral/files/spectral-latin-400-normal.woff2",
  "spectral-400i.woff2": "@fontsource/spectral/files/spectral-latin-400-italic.woff2",
  "spectral-500.woff2": "@fontsource/spectral/files/spectral-latin-500-normal.woff2",
  "spectral-600.woff2": "@fontsource/spectral/files/spectral-latin-600-normal.woff2",
  "plexmono-400.woff2": "@fontsource/ibm-plex-mono/files/ibm-plex-mono-latin-400-normal.woff2",
  "plexmono-500.woff2": "@fontsource/ibm-plex-mono/files/ibm-plex-mono-latin-500-normal.woff2",
};
for (const [name, src] of Object.entries(fonts)) {
  fs.copyFileSync(path.join(here, "node_modules", src), path.join(out, "fonts", name));
}

let count = 0;
for (const d of DOCS) {
  const md = fs.readFileSync(path.join(repo, "docs", d.file), "utf8");
  const { html, headings, h1 } = render(md);
  const page = docPage({ title: h1 || d.title, file: d.file, html, headings, description: firstParagraph(md) });
  fs.writeFileSync(path.join(out, "docs", d.file.replace(/\.md$/, ".html")), page);
  count++;
}
fs.writeFileSync(path.join(out, ".nojekyll"), "");
fs.writeFileSync(path.join(out, "404.html"), fs.readFileSync(path.join(here, "src", "index.html"), "utf8")
  .replace(/<main id="main">[\s\S]*<\/main>/, '<main id="main"><section class="hero"><div><h1>Page not found.</h1><p class="lede">The page you asked for does not exist. Start from the <a href="/">home page</a> or the documentation.</p></div></section></main>'));
console.log(`built ${count} doc pages into ${out}`);
