import { copyFile, mkdir } from 'node:fs/promises';

const output = new URL('../internal/web/static/', import.meta.url);
await mkdir(output, { recursive: true });
for (const [source, target] of [
  ['alpinejs/dist/cdn.min.js', 'alpine.min.js'],
  ['htmx.org/dist/htmx.min.js', 'htmx.min.js'],
]) {
  await copyFile(new URL(`../node_modules/${source}`, import.meta.url), new URL(target, output));
}
