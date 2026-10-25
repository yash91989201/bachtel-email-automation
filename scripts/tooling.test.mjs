import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { chmod, copyFile, mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';

async function fixture(t) {
  const root = await mkdtemp(join(tmpdir(), 'bachtel-tools-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  await mkdir(join(root, 'bin'));
  for (const file of ['Makefile', '.golangci-lint-version']) {
    await copyFile(new URL(`../${file}`, import.meta.url), join(root, file));
  }
  const version = (await readFile(join(root, '.golangci-lint-version'), 'utf8')).trim().replace(/^v/, '');
  async function executable(name, body) {
    const path = join(root, 'bin', name);
    await writeFile(path, `#!/bin/sh\n${body}\n`);
    await chmod(path, 0o755);
  }
  function make(target) {
    return spawnSync('make', ['--no-print-directory', target, `TOOLS_DIR=${join(root, 'bin')}`], {
      cwd: root,
      env: { ...process.env, PATH: `${join(root, 'bin')}:${process.env.PATH}` },
      encoding: 'utf8',
      timeout: 10_000,
    });
  }
  return { root, version, executable, make };
}

test('lint version gate accepts the pin and rejects a shadowing or broken executable', async t => {
  const { version, executable, make } = await fixture(t);
  await executable('golangci-lint', `echo '${version}'`);
  assert.equal(make('check-lint-version').status, 0);

  await executable('golangci-lint', "echo '0.0.0'");
  const mismatch = make('check-lint-version');
  assert.notEqual(mismatch.status, 0);
  assert.match(mismatch.stderr, /required; found 0\.0\.0/);
  assert.match(mismatch.stderr, /make tools/);

  await executable('golangci-lint', 'exit 127');
  const unavailable = make('check-lint-version');
  assert.notEqual(unavailable.status, 0);
  assert.match(unavailable.stderr, /Install the pinned linter/);
});

test('tools reuses a matching executable without downloading and fails on a download error', async t => {
  const { root, version, executable, make } = await fixture(t);
  await executable('golangci-lint', `echo '${version}'`);
  await executable('curl', 'exit 22');
  assert.equal(make('tools').status, 0, 'an installed matching version needs no network');

  await executable('golangci-lint', "echo '0.0.0'");
  assert.notEqual(make('tools').status, 0, 'a failed download must fail setup');
  assert.equal((await readFile(join(root, 'bin', 'golangci-lint'), 'utf8')).includes('0.0.0'), true);
});

test('Bun configuration keeps application dotenv files out of checks', async t => {
  const { root } = await fixture(t);
  await copyFile(new URL('../bunfig.toml', import.meta.url), join(root, 'bunfig.toml'));
  await writeFile(join(root, '.env'), 'BACHTEL_DOTENV_PROBE=loaded\n');
  const env = { ...process.env };
  delete env.BACHTEL_DOTENV_PROBE;
  const result = spawnSync(process.execPath, ['-e', 'if (process.env.BACHTEL_DOTENV_PROBE) process.exit(1)'], {
    cwd: root, env, encoding: 'utf8', timeout: 10_000,
  });
  assert.equal(result.status, 0, result.stderr);
});
