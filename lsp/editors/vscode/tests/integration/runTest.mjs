// Integration test runner: builds the real yammm-lsp server, transpiles the
// mocha suite, and launches an Extension Development Host against the fixture
// workspace. The server binary directory is PATH-prepended for the spawned
// VS Code process so the extension's PATH fallback resolves the fresh build.
import { spawnSync } from 'node:child_process';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { fileURLToPath } from 'node:url';
import * as esbuild from 'esbuild';
import { runTests } from '@vscode/test-electron';

const extensionRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const repoRoot = path.resolve(extensionRoot, '..', '..', '..');
const outDir = path.join(extensionRoot, 'out-test');
const binDir = path.join(outDir, 'bin');
const binaryName = process.platform === 'win32' ? 'yammm-lsp.exe' : 'yammm-lsp';

function buildServerBinary() {
    fs.mkdirSync(binDir, { recursive: true });
    const result = spawnSync('go', ['build', '-o', path.join(binDir, binaryName), './cmd/yammm-lsp'], {
        cwd: repoRoot,
        stdio: 'inherit',
    });
    if (result.error) {
        throw result.error;
    }
    if (result.status !== 0) {
        throw new Error(`go build of yammm-lsp failed with status ${result.status}`);
    }
}

function buildExtensionBundle() {
    // The dev host loads out/extension.js per the manifest's main entry; a
    // stale bundle would silently test old code, so always rebuild.
    const result = spawnSync(process.execPath, [path.join(extensionRoot, 'esbuild.mjs')], {
        cwd: extensionRoot,
        stdio: 'inherit',
    });
    if (result.error) {
        throw result.error;
    }
    if (result.status !== 0) {
        throw new Error(`esbuild of the extension bundle failed with status ${result.status}`);
    }
}

async function compileSuite() {
    await esbuild.build({
        entryPoints: [
            path.join(extensionRoot, 'tests', 'integration', 'suite', 'index.ts'),
            path.join(extensionRoot, 'tests', 'integration', 'suite', 'extension.test.ts'),
        ],
        outdir: path.join(outDir, 'suite'),
        format: 'cjs',
        platform: 'node',
        target: 'ES2020',
        sourcemap: true,
    });
}

async function main() {
    buildServerBinary();
    buildExtensionBundle();
    await compileSuite();
    // The default user-data-dir lives under the (deeply nested) extension
    // directory; its IPC socket path then exceeds the 103-char Unix-socket
    // limit on macOS. A short fixed temp path keeps the socket addressable.
    const userDataDir = path.join(os.tmpdir(), 'yammm-vscode-test');
    fs.mkdirSync(userDataDir, { recursive: true });
    await runTests({
        extensionDevelopmentPath: extensionRoot,
        extensionTestsPath: path.join(outDir, 'suite', 'index.js'),
        launchArgs: [
            path.join(extensionRoot, 'tests', 'integration', 'fixtures', 'workspace'),
            `--user-data-dir=${userDataDir}`,
            '--disable-extensions',
            '--disable-workspace-trust',
            '--skip-welcome',
            '--skip-release-notes',
        ],
        extensionTestsEnv: {
            PATH: `${binDir}${path.delimiter}${process.env.PATH ?? ''}`,
        },
    });
}

main().catch((err) => {
    console.error('Integration tests failed:', err);
    process.exit(1);
});
