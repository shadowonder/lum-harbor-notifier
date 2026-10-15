import { createHash } from 'node:crypto';
import { mkdirSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { buildSync } from 'esbuild';
import { createElement, type ComponentType } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

/**
 * Real component rendering for node:test without a DOM or E2E framework: the
 * production .tsx module is compiled by Vite's own esbuild (JSX + TypeScript)
 * into node_modules/.cache, with packages left external so React and MUI
 * resolve to the single installed copy, then rendered with react-dom/server.
 */
const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../..');

export async function loadComponent<P extends object>(sourcePath: string): Promise<ComponentType<P>> {
  const entry = resolve(frontendRoot, sourcePath);
  const outDir = join(frontendRoot, 'node_modules', '.cache', 'lum-render-tests');
  mkdirSync(outDir, { recursive: true });
  const outfile = join(outDir, `${createHash('sha1').update(entry).digest('hex')}.mjs`);
  buildSync({
    entryPoints: [entry],
    outfile,
    bundle: true,
    format: 'esm',
    platform: 'node',
    jsx: 'automatic',
    packages: 'external',
    loader: { '.css': 'empty', '.svg': 'empty', '.png': 'empty' },
    logLevel: 'error',
  });
  const module = (await import(`${outfile}?t=${Date.now()}`)) as { default: ComponentType<P> };
  return module.default;
}

export function renderMarkup<P extends object>(component: ComponentType<P>, props: P): string {
  return renderToStaticMarkup(createElement(component, props));
}

/** Visible text of rendered markup: styles dropped, tags removed, entities decoded. */
export function visibleText(markup: string): string {
  return markup
    .replace(/<style[^>]*>[\s\S]*?<\/style>/g, ' ')
    .replace(/<[^>]+>/g, ' ')
    .replace(/&amp;/g, '&')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&#x27;|&#39;/g, "'")
    .replace(/\s+/g, ' ')
    .trim();
}
