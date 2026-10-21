import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { copyFile, mkdtemp, mkdir, readFile, rm, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { applyEdits, lintTailwind } from './lint-tailwind.mjs';

test('checks and fixes templ files using real Tailwind code actions', { timeout: 60_000 }, async () => {
  const root = await mkdtemp(join(tmpdir(), 'bachtel-tailwind-'));
  try {
    await mkdir(join(root, 'web/css'), { recursive: true });
    await symlink(fileURLToPath(new URL('../node_modules', import.meta.url)), join(root, 'node_modules'), 'dir');
    await writeFile(join(root, 'web/css/app.css'), '@import "tailwindcss";\n');
    await writeFile(join(root, 'clean.templ'), 'package test\ntempl Clean() { <div class="max-w-310 px-4.5 pb-11"></div> }\n');
    await writeFile(join(root, 'warnings.templ'), 'package test\ntempl Warnings() { <div class="max-w-[1240px] [&>*]:min-w-0 max-[760px]:px-[18px] max-[760px]:pb-[44px]"></div> }\n');
    const { diagnostics } = await lintTailwind(['clean.templ', 'warnings.templ'], root);
    assert.ok(diagnostics.length >= 4);
    assert.ok(diagnostics.every(d => d.file === 'warnings.templ'));
    for (const suggestion of ['max-w-310', '*:min-w-0', 'max-[760px]:px-4.5', 'max-[760px]:pb-11']) {
      assert.ok(diagnostics.some(d => d.code === 'suggestCanonicalClasses' && d.message.includes(suggestion)), suggestion);
    }
    const original = await readFile(join(root, 'warnings.templ'), 'utf8');
    assert.ok(original.includes('max-w-[1240px]'), 'checking must not edit source');
    const conflict = 'package test\ntempl Conflict() { <div class="p-2 p-4"></div> }\n';
    await writeFile(join(root, 'conflict.templ'), conflict);
    const fixed = await lintTailwind(['clean.templ', 'warnings.templ', 'conflict.templ'], root, { fix: true });
    assert.ok(fixed.diagnostics.length > 0);
    assert.ok(fixed.diagnostics.every(d => d.file === 'conflict.templ' && d.code === 'cssConflict'));
    assert.deepEqual(fixed.fixedFiles, ['warnings.templ']);
    assert.equal(await readFile(join(root, 'conflict.templ'), 'utf8'), conflict, 'conflicting classes must not be deleted');
    const updated = await readFile(join(root, 'warnings.templ'), 'utf8');
    assert.equal(updated, original.replace('max-w-[1240px] [&>*]:min-w-0 max-[760px]:px-[18px] max-[760px]:pb-[44px]', 'max-w-310 *:min-w-0 max-[760px]:px-4.5 max-[760px]:pb-11'));
    assert.deepEqual(await lintTailwind(['clean.templ', 'warnings.templ'], root, { fix: true }), { diagnostics: [], fixedFiles: [] });
    // Exercise the actual CLI and --fix flag in an isolated repository too.
    await mkdir(join(root, 'scripts'));
    await copyFile(new URL('./lint-tailwind.mjs', import.meta.url), join(root, 'scripts/lint-tailwind.mjs'));
    assert.equal(spawnSync('git', ['init', '--quiet', root]).status, 0);
    await writeFile(join(root, 'warnings.templ'), original);
    const cli = spawnSync(process.execPath, [join(root, 'scripts/lint-tailwind.mjs'), '--fix'], { cwd: root, encoding: 'utf8', timeout: 30_000 });
    assert.equal(cli.status, 1, cli.stderr);
    assert.match(cli.stdout, /Fixed canonical Tailwind classes: warnings.templ/);
    assert.match(cli.stdout, /cssConflict/);
    assert.equal(await readFile(join(root, 'warnings.templ'), 'utf8'), updated);
    await assert.rejects(lintTailwind([], root), /No .templ files/);
    await assert.rejects(lintTailwind(['missing.templ'], root), /ENOENT/);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test('applies UTF-16 edit positions without shifting later edits or CRLFs', () => {
  const edit = (line, start, end, newText) => ({ range: { start: { line, character: start }, end: { line, character: end } }, newText });
  assert.equal(applyEdits('😀 abc def\r\nxyz\r\n', [edit(0, 3, 6, 'longer'), edit(0, 7, 10, 'd'), edit(1, 0, 3, 'q')]), '😀 longer d\r\nq\r\n');
  assert.throws(() => applyEdits('abcdef', [edit(0, 1, 4, 'x'), edit(0, 3, 5, 'y')]), /overlapping/);
  assert.throws(() => applyEdits('abc', [edit(2, 0, 1, 'x')]), /invalid edit position/);
});

test('CLI rejects unknown flags with a usage error', () => {
  const result = spawnSync(process.execPath, [fileURLToPath(new URL('./lint-tailwind.mjs', import.meta.url)), '--unknown'], { encoding: 'utf8' });
  assert.equal(result.status, 2);
  assert.match(result.stderr, /Usage:/);
});
