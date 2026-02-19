import * as path from 'path';
import * as os from 'os';
import * as fs from 'fs';
import {
    workspace,
    ExtensionContext,
    window,
    OutputChannel,
} from 'vscode';
import {
    LanguageClient,
    LanguageClientOptions,
    ServerOptions,
    TransportKind,
    Trace,
} from 'vscode-languageclient/node';

let client: LanguageClient | undefined;
let outputChannel: OutputChannel;

export function activate(context: ExtensionContext) {
    outputChannel = window.createOutputChannel('YAMMM');
    context.subscriptions.push(outputChannel);
    outputChannel.appendLine('YAMMM extension activating...');

    const serverPath = getServerPath(context);
    if (!serverPath) {
        window.showErrorMessage(
            'YAMMM: Could not find yammm-lsp binary. Please configure yammm.lsp.serverPath or install the binary.'
        );
        return;
    }

    outputChannel.appendLine(`Using server: ${serverPath}`);

    // Verify the binary exists
    if (!fs.existsSync(serverPath)) {
        window.showErrorMessage(
            `YAMMM: Server binary not found at ${serverPath}`
        );
        return;
    }

    startLanguageServer(context, serverPath);

    // Listen for configuration changes (3.3)
    context.subscriptions.push(
        workspace.onDidChangeConfiguration(async (e) => {
            if (e.affectsConfiguration('yammm.lsp') || e.affectsConfiguration('yammm.trace')) {
                const action = await window.showInformationMessage(
                    'YAMMM configuration changed. Restart language server?',
                    'Restart',
                    'Later'
                );
                if (action === 'Restart') {
                    await restartLanguageClient(context);
                }
            }
        })
    );
}

// Helper to convert trace level string to Trace enum (3.4)
function convertTraceLevel(level: string): Trace {
    switch (level) {
        case 'messages':
            return Trace.Messages;
        case 'verbose':
            return Trace.Verbose;
        default:
            return Trace.Off;
    }
}

// Restart the language client when configuration changes (3.3)
async function restartLanguageClient(context: ExtensionContext): Promise<void> {
    if (client) {
        await client.stop();
        client = undefined;
    }
    const serverPath = getServerPath(context);
    if (serverPath) {
        startLanguageServer(context, serverPath);
    }
}

function getServerPath(context: ExtensionContext): string | undefined {
    // First check user configuration
    // Pass first workspace folder as scope so folder-level .vscode/settings.json is included
    const config = workspace.getConfiguration('yammm.lsp', workspace.workspaceFolders?.[0]?.uri);
    const configuredPath = config.get<string>('serverPath');
    if (configuredPath && configuredPath.length > 0) {
        // Handle ~ expansion for user home directory
        let resolvedPath: string | undefined;
        if (configuredPath === '~') {
            // Bare ~ is a directory, not a binary - warn and fall through
            outputChannel.appendLine(
                'Warning: serverPath "~" is a directory, not a binary path. Falling back to bundled/PATH lookup.'
            );
            resolvedPath = undefined;
        } else if (configuredPath.startsWith('~/')) {
            resolvedPath = path.join(os.homedir(), configuredPath.slice(2));
        } else {
            resolvedPath = configuredPath;
        }

        // Validate the configured path if we have one
        if (resolvedPath) {
            // Resolve relative paths against first workspace folder
            // NOTE: In multi-root workspaces, use absolute paths for predictable behavior
            if (!path.isAbsolute(resolvedPath)) {
                const folders = workspace.workspaceFolders;
                if (folders && folders.length > 1) {
                    outputChannel.appendLine(
                        `Warning: serverPath is relative and multiple workspace folders detected. ` +
                            `Using first folder: ${folders[0].uri.fsPath}. Consider using an absolute path.`
                    );
                }
                const workspaceRoot = folders?.[0]?.uri.fsPath;
                if (workspaceRoot) {
                    resolvedPath = path.resolve(workspaceRoot, resolvedPath);
                } else {
                    outputChannel.appendLine(
                        `Warning: serverPath is relative but no workspace folder is open. Falling back to bundled/PATH lookup.`
                    );
                    resolvedPath = undefined;
                }
            }
        }

        if (resolvedPath) {
            if (!fs.existsSync(resolvedPath)) {
                outputChannel.appendLine(
                    `Warning: Configured serverPath does not exist: ${resolvedPath}. Falling back to bundled/PATH lookup.`
                );
            } else {
                // Verify it's a file, not a directory (directories can pass X_OK on Unix)
                // Wrap in try-catch to handle race condition if file is deleted between existsSync and statSync
                try {
                    const stat = fs.statSync(resolvedPath);
                    if (!stat.isFile()) {
                        outputChannel.appendLine(
                            `Warning: Configured serverPath is a directory, not a file: ${resolvedPath}. Falling back to bundled/PATH lookup.`
                        );
                    } else if (process.platform !== 'win32') {
                        // On Unix, verify execute permission (matching PATH lookup behavior)
                        try {
                            fs.accessSync(resolvedPath, fs.constants.X_OK);
                            return resolvedPath;
                        } catch {
                            outputChannel.appendLine(
                                `Warning: Configured serverPath is not executable: ${resolvedPath}. Falling back to bundled/PATH lookup.`
                            );
                        }
                    } else {
                        // Windows: no X_OK check needed
                        return resolvedPath;
                    }
                } catch {
                    outputChannel.appendLine(
                        `Warning: Could not stat serverPath (file may have been removed): ${resolvedPath}. Falling back to bundled/PATH lookup.`
                    );
                }
            }
        }
        // Fall through to bundled/PATH lookup if configured path is invalid
    }

    // Try to find bundled binary
    const platform = os.platform();
    const arch = os.arch();

    let binaryName = 'yammm-lsp';
    if (platform === 'win32') {
        binaryName = 'yammm-lsp.exe';
    }

    // Map platform/arch to binary directory name
    let platformDir: string;
    switch (platform) {
        case 'darwin':
            platformDir = arch === 'arm64' ? 'darwin-arm64' : 'darwin-amd64';
            break;
        case 'linux':
            platformDir = arch === 'arm64' ? 'linux-arm64' : 'linux-amd64';
            break;
        case 'win32':
            platformDir = arch === 'arm64' ? 'windows-arm64' : 'windows-amd64';
            break;
        default:
            outputChannel.appendLine(`Unsupported platform: ${platform}`);
            return undefined;
    }

    const bundledPath = context.asAbsolutePath(
        path.join('bin', platformDir, binaryName)
    );

    if (fs.existsSync(bundledPath)) {
        // Ensure executable bit is set (Unix-like systems)
        if (process.platform !== 'win32') {
            try {
                fs.accessSync(bundledPath, fs.constants.X_OK);
            } catch {
                // Not executable, try to fix it
                try {
                    fs.chmodSync(bundledPath, 0o755);
                    outputChannel.appendLine(`Set executable permission on ${bundledPath}`);
                } catch (chmodErr) {
                    outputChannel.appendLine(`Warning: Could not set executable permission: ${chmodErr}`);
                }
            }
        }
        return bundledPath;
    }

    // Try PATH as fallback
    const pathEnv = process.env.PATH || '';
    const pathDirs = pathEnv.split(path.delimiter);
    for (const dir of pathDirs) {
        const candidate = path.join(dir, binaryName);
        if (fs.existsSync(candidate)) {
            // Verify it's a file, not a directory (directories can pass X_OK on Unix)
            try {
                const stat = fs.statSync(candidate);
                if (!stat.isFile()) {
                    continue; // Skip directories and special files
                }
            } catch {
                continue; // File was deleted or error checking - skip
            }

            // On Unix, verify execute permission (3.2)
            if (process.platform !== 'win32') {
                try {
                    fs.accessSync(candidate, fs.constants.X_OK);
                } catch {
                    continue; // Skip non-executable files
                }
            }
            return candidate;
        }
    }

    return undefined;
}

function startLanguageServer(context: ExtensionContext, serverPath: string) {
    // Pass first workspace folder as scope so folder-level .vscode/settings.json is included
    const folderScope = workspace.workspaceFolders?.[0]?.uri;
    const config = workspace.getConfiguration('yammm.lsp', folderScope);
    const logLevel = config.get<string>('logLevel', 'info');
    const logFileConfig = config.get<string>('logFile', '');
    const moduleRootConfig = config.get<string>('moduleRoot', '');
    // Read trace setting (3.4)
    const traceConfig = workspace.getConfiguration('yammm.trace', folderScope);
    const traceLevel = traceConfig.get<string>('server', 'off');

    // Resolve moduleRoot with ~ expansion and relative path handling
    let moduleRoot: string | undefined;
    if (moduleRootConfig && moduleRootConfig.length > 0) {
        if (moduleRootConfig === '~') {
            // ~ alone is valid (home directory)
            moduleRoot = os.homedir();
        } else if (moduleRootConfig.startsWith('~/')) {
            moduleRoot = path.join(os.homedir(), moduleRootConfig.slice(2));
        } else {
            moduleRoot = moduleRootConfig;
        }

        // Resolve relative paths against first workspace folder
        if (moduleRoot && !path.isAbsolute(moduleRoot)) {
            const folders = workspace.workspaceFolders;
            if (folders && folders.length > 1) {
                outputChannel.appendLine(
                    `Warning: moduleRoot is relative with multiple workspace folders. ` +
                    `Using first folder: ${folders[0].uri.fsPath}.`
                );
            }
            const workspaceRoot = folders?.[0]?.uri.fsPath;
            if (workspaceRoot) {
                moduleRoot = path.resolve(workspaceRoot, moduleRoot);
            } else {
                outputChannel.appendLine(
                    `Warning: moduleRoot is relative but no workspace folder is open. Ignoring.`
                );
                moduleRoot = undefined;
            }
        }

        // Verify path exists and is a directory
        if (moduleRoot) {
            if (!fs.existsSync(moduleRoot)) {
                outputChannel.appendLine(
                    `Warning: moduleRoot does not exist: ${moduleRoot}. Ignoring.`
                );
                moduleRoot = undefined;
            } else {
                try {
                    const stat = fs.statSync(moduleRoot);
                    if (!stat.isDirectory()) {
                        outputChannel.appendLine(
                            `Warning: moduleRoot is not a directory: ${moduleRoot}. Ignoring.`
                        );
                        moduleRoot = undefined;
                    }
                } catch {
                    outputChannel.appendLine(
                        `Warning: Could not stat moduleRoot: ${moduleRoot}. Ignoring.`
                    );
                    moduleRoot = undefined;
                }
            }
        }
    }

    // Resolve logFile with ~ expansion and relative path handling
    let logFile: string | undefined;
    if (logFileConfig && logFileConfig.length > 0) {
        if (logFileConfig.startsWith('~/')) {
            logFile = path.join(os.homedir(), logFileConfig.slice(2));
        } else {
            logFile = logFileConfig;
        }

        // Resolve relative paths against first workspace folder
        if (logFile && !path.isAbsolute(logFile)) {
            const folders = workspace.workspaceFolders;
            if (folders && folders.length > 1) {
                outputChannel.appendLine(
                    `Warning: logFile is relative with multiple workspace folders. ` +
                    `Using first folder: ${folders[0].uri.fsPath}.`
                );
            }
            const workspaceRoot = folders?.[0]?.uri.fsPath;
            if (workspaceRoot) {
                logFile = path.resolve(workspaceRoot, logFile);
            } else {
                outputChannel.appendLine(
                    `Warning: logFile is relative but no workspace folder is open. Ignoring.`
                );
                logFile = undefined;
            }
        }

        // Verify parent directory exists (file will be created by server)
        if (logFile) {
            const logDir = path.dirname(logFile);
            if (!fs.existsSync(logDir)) {
                outputChannel.appendLine(
                    `Warning: logFile parent directory does not exist: ${logDir}. Ignoring.`
                );
                logFile = undefined;
            }
        }

        if (logFile) {
            outputChannel.appendLine(`Using log file: ${logFile}`);
        }
    }

    // Build server arguments (base args without --log-level to avoid duplication in debug mode)
    const baseArgs: string[] = [];
    if (moduleRoot) {
        baseArgs.push('--module-root', moduleRoot);
    }
    if (logFile) {
        baseArgs.push('--log-file', logFile);
    }

    const serverOptions: ServerOptions = {
        run: {
            command: serverPath,
            args: [...baseArgs, '--log-level', logLevel],
            transport: TransportKind.stdio,
        },
        debug: {
            command: serverPath,
            args: [...baseArgs, '--log-level', 'debug'],
            transport: TransportKind.stdio,
        },
    };

    const clientOptions: LanguageClientOptions = {
        documentSelector: [
            { scheme: 'file', language: 'yammm' },
            { scheme: 'file', language: 'markdown' },
        ],
        synchronize: {
            fileEvents: workspace.createFileSystemWatcher('**/*.yammm'),
        },
        outputChannel: outputChannel,
        traceOutputChannel: outputChannel,
    };

    client = new LanguageClient(
        'yammm',
        'YAMMM Language Server',
        serverOptions,
        clientOptions
    );

    // Apply trace level (3.4)
    client.setTrace(convertTraceLevel(traceLevel));

    // Start the client (also starts the server)
    // Note: No disposable is pushed here. Cleanup is handled by:
    // - restartLanguageClient() when restarting
    // - deactivate() when the extension is deactivated
    // This avoids accumulating disposables on each restart.
    client.start().then(() => {
        outputChannel.appendLine('YAMMM Language Server started');
    }).catch((error) => {
        outputChannel.appendLine(`Failed to start language server: ${error}`);
        window.showErrorMessage(`YAMMM: Failed to start language server: ${error}`);
    });
}

export function deactivate(): Thenable<void> | undefined {
    if (!client) {
        return undefined;
    }
    return client.stop();
}
