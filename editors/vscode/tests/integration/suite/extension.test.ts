import * as assert from 'assert';
import * as vscode from 'vscode';

function fixtureUri(name: string): vscode.Uri {
    const folder = vscode.workspace.workspaceFolders?.[0];
    assert.ok(folder, 'fixture workspace folder is open');
    return vscode.Uri.joinPath(folder.uri, name);
}

function errorDiagnostics(uri: vscode.Uri): vscode.Diagnostic[] {
    return vscode.languages
        .getDiagnostics(uri)
        .filter((d) => d.severity === vscode.DiagnosticSeverity.Error);
}

async function pollUntil<T>(
    fn: () => T | undefined,
    timeoutMs: number,
    intervalMs = 250
): Promise<T | undefined> {
    const deadline = Date.now() + timeoutMs;
    for (;;) {
        const value = fn();
        if (value !== undefined) {
            return value;
        }
        if (Date.now() > deadline) {
            return undefined;
        }
        await new Promise((resolve) => setTimeout(resolve, intervalMs));
    }
}

suite('YAMMM extension integration', () => {
    test('extension activates', async () => {
        const ext = vscode.extensions.getExtension('simon-lentz.yammm');
        assert.ok(ext, 'extension simon-lentz.yammm is installed in the dev host');
        await ext.activate();
        assert.ok(ext.isActive, 'extension reports active');
    });

    // End-to-end startup signal: diagnostics only arrive if the language
    // client started, the server analyzed the document, and the workspace
    // published the result back over the protocol.
    test('language server publishes diagnostics for a broken schema', async () => {
        const uri = fixtureUri('broken.yammm');
        const doc = await vscode.workspace.openTextDocument(uri);
        await vscode.window.showTextDocument(doc);

        const errors = await pollUntil(() => {
            const diags = errorDiagnostics(uri);
            return diags.length > 0 ? diags : undefined;
        }, 30_000);

        assert.ok(
            errors !== undefined && errors.length > 0,
            'expected at least one error diagnostic for broken.yammm (E_NO_PRIMARY_KEY)'
        );
    });

    test('valid schema stays free of error diagnostics', async () => {
        const uri = fixtureUri('valid.yammm');
        const doc = await vscode.workspace.openTextDocument(uri);
        await vscode.window.showTextDocument(doc);

        // The pipeline is proven live by the broken-schema test; a short
        // settle window guards against late-arriving false positives here.
        await new Promise((resolve) => setTimeout(resolve, 2_000));
        const errors = errorDiagnostics(uri);
        assert.strictEqual(
            errors.length,
            0,
            `expected no error diagnostics for valid.yammm, got: ${errors
                .map((d) => d.message)
                .join('; ')}`
        );
    });

    test('language server provides hover for a type declaration', async () => {
        const uri = fixtureUri('valid.yammm');
        const doc = await vscode.workspace.openTextDocument(uri);
        await vscode.window.showTextDocument(doc);

        // Line 2, character 7 is the "P" of "type Person {". Poll because
        // the server may still be analyzing when the document first opens.
        const position = new vscode.Position(2, 7);
        const deadline = Date.now() + 30_000;
        let hovers: vscode.Hover[] = [];
        for (;;) {
            hovers = await vscode.commands.executeCommand<vscode.Hover[]>(
                'vscode.executeHoverProvider',
                uri,
                position
            );
            if (hovers.length > 0 || Date.now() > deadline) {
                break;
            }
            await new Promise((resolve) => setTimeout(resolve, 250));
        }

        assert.ok(
            hovers.length > 0,
            'expected a hover result for the Person type declaration'
        );
        const text = hovers
            .flatMap((h) => h.contents)
            .map((c) => (typeof c === 'string' ? c : c.value))
            .join('\n');
        assert.ok(
            text.includes('Person'),
            `hover content should mention the type name, got: ${text}`
        );
    });
});
