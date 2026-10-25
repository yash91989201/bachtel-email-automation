import { spawn, execFileSync } from 'node:child_process';
import { readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { createMessageConnection, StreamMessageReader, StreamMessageWriter } from 'vscode-jsonrpc/node.js';

const root = fileURLToPath(new URL('../', import.meta.url));

export function applyEdits(text, edits) {
  const lines = [0, ...Array.from(text.matchAll(/\n/g), match => match.index + 1)];
  function offset({ line, character }) {
    const start = lines[line];
    const end = (lines[line + 1] ?? text.length + 1) - 1;
    if (!Number.isInteger(line) || !Number.isInteger(character) || character < 0 || start === undefined || start + character > end) {
      throw new Error('Tailwind returned an invalid edit position.');
    }
    return start + character;
  }
  const replacements = edits.map(({ range, newText }) => ({ start: offset(range.start), end: offset(range.end), newText }))
    .sort((a, b) => b.start - a.start);
  let previousStart = text.length;
  for (const { start, end, newText } of replacements) {
    if (end < start || end > previousStart) throw new Error('Tailwind returned overlapping or reversed edits.');
    text = text.slice(0, start) + newText + text.slice(end);
    previousStart = start;
  }
  return text;
}

// Use the pinned server's readiness hooks, also used by its integration tests.
// JSON-RPC owns framing and request errors; no custom protocol parser is needed.
export async function lintTailwind(files, cwd = root, { fix = false } = {}) {
  if (files.length === 0) throw new Error('No .templ files found; nothing was checked.');
  const server = spawn(process.execPath, [
    resolve(root, 'node_modules/@tailwindcss/language-server/bin/tailwindcss-language-server'), '--stdio',
  ], { cwd, stdio: ['pipe', 'pipe', 'pipe'] });
  const connection = createMessageConnection(new StreamMessageReader(server.stdout), new StreamMessageWriter(server.stdin));
  const settings = {
    tailwindCSS: {
      validate: true,
      codeActions: true,
      includeLanguages: { templ: 'html' },
      lint: { suggestCanonicalClasses: 'warning' },
      experimental: { configFile: { 'web/css/app.css': '**/*.templ' } },
    },
  };
  let stderr = '';
  server.stderr.on('data', chunk => { stderr = (stderr + chunk).slice(-8000); });
  const pending = new Map();
  function notification(method, uri = '') {
    return new Promise(resolve => { pending.set(`${method}:${uri}`, resolve); });
  }
  connection.onNotification((method, params) => {
    if (method === 'window/logMessage') stderr = (stderr + '\n' + params.message).slice(-8000);
    const key = `${method}:${params?.uri ?? ''}`;
    pending.get(key)?.(params);
    pending.delete(key);
  });
  connection.onRequest('workspace/configuration', ({ items }) => items.map(({ section }) =>
    section ? section.split('.').reduce((value, key) => value?.[key], settings) ?? null : settings));
  connection.onRequest('client/registerCapability', () => null);
  connection.onRequest('workspace/workspaceFolders', () => [{ uri: pathToFileURL(cwd).href, name: 'bachtel' }]);
  connection.listen();
  let timer;
  const failure = new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error(`Tailwind check timed out; results are incomplete. ${stderr}`)), 120_000);
    server.on('error', reject);
    server.on('exit', (code, signal) => reject(new Error(`Tailwind server exited early (${signal ?? code}). ${stderr}`)));
  });
  const run = async () => {
    await connection.sendRequest('initialize', {
      processId: process.pid,
      rootUri: pathToFileURL(cwd).href,
      capabilities: { workspace: { configuration: true, workspaceFolders: true } },
      initializationOptions: { testMode: true, userLanguages: { templ: 'html' } },
    });
    const ready = notification('@/tailwindCSS/serverReady');
    await connection.sendNotification('initialized', {});
    await ready;
    const diagnostics = [];
    const fixedFiles = [];
    for (const file of files) {
      const uri = pathToFileURL(resolve(cwd, file)).href;
      const original = await readFile(resolve(cwd, file), 'utf8');
      const published = notification('textDocument/publishDiagnostics', uri);
      const documentReady = notification('@/tailwindCSS/documentReady', uri);
      await connection.sendNotification('textDocument/didOpen', {
        textDocument: { uri, languageId: 'templ', version: 1, text: original },
      });
      await documentReady;
      // No matching/enabled project must never count as a clean file.
      const project = await connection.sendRequest('@/tailwindCSS/getProject', { uri });
      if (!project) throw new Error(`No Tailwind project for ${file}; results are incomplete.`);
      let result = await published;
      const canonical = result.diagnostics.filter(d => d.code === 'suggestCanonicalClasses' && d.suggestions?.length === 1);
      if (fix && canonical.length) {
        const lines = original.split('\n');
        const actions = await connection.sendRequest('textDocument/codeAction', {
          textDocument: { uri },
          range: { start: { line: 0, character: 0 }, end: { line: lines.length - 1, character: lines.at(-1).length } },
          context: { diagnostics: canonical, only: ['quickfix'] },
        });
        const edits = [];
        for (const action of actions ?? []) {
          if (action.disabled || !action.diagnostics?.length || !action.diagnostics.every(d => d.code === 'suggestCanonicalClasses')) continue;
          if (action.command || action.edit?.documentChanges || Object.keys(action.edit?.changes ?? {}).some(key => key !== uri)) {
            throw new Error(`Unsupported Tailwind code action for ${file}.`);
          }
          edits.push(...(action.edit?.changes?.[uri] ?? []));
        }
        const updated = applyEdits(original, edits);
        if (updated !== original) {
          if (await readFile(resolve(cwd, file), 'utf8') !== original) throw new Error(`${file} changed during analysis; refusing to overwrite it.`);
          await writeFile(resolve(cwd, file), updated);
          fixedFiles.push(file);
          const checked = notification('textDocument/publishDiagnostics', uri);
          await connection.sendNotification('textDocument/didChange', {
            textDocument: { uri, version: 2 }, contentChanges: [{ text: updated }],
          });
          result = await checked;
        }
      }
      for (const diagnostic of result.diagnostics) {
        if (diagnostic.severity === 1 || diagnostic.severity === 2) {
          diagnostics.push({ file, ...diagnostic });
        }
      }
      await connection.sendNotification('textDocument/didClose', { textDocument: { uri } });
    }
    return { diagnostics, fixedFiles };
  };
  try {
    return await Promise.race([run(), failure]);
  } finally {
    clearTimeout(timer);
    connection.dispose();
    server.kill();
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const args = process.argv.slice(2);
    if (args.some(arg => arg !== '--fix')) throw new Error('Usage: bun run lint:tailwind [--fix]');
    const files = execFileSync('git', ['ls-files', '--cached', '--others', '--exclude-standard', '-z', '--', '*.templ'], { cwd: root })
      .toString().split('\0').filter(Boolean).sort();
    const { diagnostics, fixedFiles } = await lintTailwind(files, root, { fix: args.includes('--fix') });
    for (const file of fixedFiles) console.log(`Fixed canonical Tailwind classes: ${file}`);
    for (const { file, range, message, code } of diagnostics) {
      console.log(`${file}:${range.start.line + 1}:${range.start.character + 1}: ${message} [${code ?? 'tailwindcss'}]`);
    }
    console.log(`Checked ${files.length} .templ files: ${diagnostics.length} Tailwind diagnostic(s).`);
    process.exitCode = diagnostics.length ? 1 : 0;
  } catch (error) {
    console.error(`Tailwind check failed: ${error.message}`);
    process.exitCode = 2;
  }
}
