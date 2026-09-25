const vscode = require('vscode');
const path = require('path');
const fs = require('fs');
const os = require('os');
const cp = require('child_process');

/**
 * Resolve the best idlistack binary to use:
 * 1. Check system PATH (if user installed via install.sh or package manager)
 * 2. Fall back to bundled extension bin/idlistack
 * @param {string} extensionPath 
 * @returns {string} Absolute path to executable
 */
function resolveCliPath(extensionPath) {
    const isWin = os.platform() === 'win32';
    const binName = isWin ? 'idlistack.exe' : 'idlistack';

    // 1. Check if idlistack is available on host system PATH
    try {
        const checkCmd = isWin ? `where ${binName}` : `which ${binName}`;
        const stdout = cp.execSync(checkCmd, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }).trim();
        if (stdout && fs.existsSync(stdout.split('\n')[0].trim())) {
            return stdout.split('\n')[0].trim();
        }
    } catch (_) {}

    // 2. Fall back to extension bundled binary
    return path.join(extensionPath, 'bin', binName);
}

/**
 * @param {vscode.ExtensionContext} context
 */
function activate(context) {
    console.log('IdliStack extension v1.8.0 active!');

    const binDir = path.join(context.extensionPath, 'bin');
    const cliPath = resolveCliPath(context.extensionPath);
    const isWin = os.platform() === 'win32';

    // 1. Ensure bundled CLI binary is executable
    try {
        if (fs.existsSync(cliPath) && !isWin) {
            fs.chmodSync(cliPath, 0o755);
        }
    } catch (e) {
        console.error('Failed to set execute permissions on CLI binary:', e);
    }

    // 2. Global Terminal PATH Injection:
    // Injects the extension bin directory into every integrated terminal opened in VS Code!
    if (context.environmentVariableCollection) {
        context.environmentVariableCollection.prepend('PATH', `${binDir}${path.delimiter}`);
        context.environmentVariableCollection.description = 'IdliStack CLI binary path';
    }

    // 3. User Environment Registration (~/.local/bin/idlistack & shell rc persistence):
    // Allows running `idlistack` from any terminal session outside VS Code as well.
    try {
        const homeDir = os.homedir();
        const localBin = path.join(homeDir, '.local', 'bin');
        if (!fs.existsSync(localBin)) {
            fs.mkdirSync(localBin, { recursive: true });
        }
        const symlinkPath = path.join(localBin, isWin ? 'idlistack.exe' : 'idlistack');
        if (!fs.existsSync(symlinkPath) && fs.existsSync(cliPath)) {
            try {
                fs.symlinkSync(cliPath, symlinkPath);
            } catch (_) {
                // If symlink fails, copy binary
                fs.copyFileSync(cliPath, symlinkPath);
                if (!isWin) {
                    fs.chmodSync(symlinkPath, 0o755);
                }
            }
        }

        // Ensure ~/.local/bin is present in ~/.bashrc and ~/.zshrc if not already there
        if (!isWin) {
            const exportLine = '\n# IdliStack CLI path\nexport PATH="$HOME/.local/bin:$PATH"\n';
            for (const rcName of ['.bashrc', '.zshrc']) {
                const rcPath = path.join(homeDir, rcName);
                if (fs.existsSync(rcPath)) {
                    const rcContent = fs.readFileSync(rcPath, 'utf8');
                    if (!rcContent.includes('.local/bin')) {
                        fs.appendFileSync(rcPath, exportLine);
                    }
                }
            }
        }
    } catch (e) {
        console.error('Failed to register ~/.local/bin/idlistack:', e);
    }

    function getWorkspaceRoot() {
        const workspaceFolders = vscode.workspace.workspaceFolders;
        if (!workspaceFolders || workspaceFolders.length === 0) {
            vscode.window.showErrorMessage('Please open a workspace folder to use IdliStack.');
            return null;
        }
        return workspaceFolders[0].uri.fsPath;
    }

    // Helper to get or create a dedicated terminal with explicit PATH environment
    function getTerminal(name = 'IdliStack') {
        let terminal = vscode.window.terminals.find(t => t.name === name);
        if (!terminal) {
            terminal = vscode.window.createTerminal({
                name: name,
                env: {
                    PATH: `${binDir}${path.delimiter}${process.env.PATH || ''}`
                }
            });
        }
        terminal.show();
        return terminal;
    }

    // Command: idlistack.terminal (Opens dedicated terminal with idlistack pre-configured)
    let terminalDisposable = vscode.commands.registerCommand("idlistack.terminal", function () {
        const rootPath = getWorkspaceRoot();
        const terminal = vscode.window.createTerminal({
            name: 'IdliStack Terminal',
            cwd: rootPath || undefined,
            env: {
                PATH: `${binDir}${path.delimiter}${process.env.PATH || ''}`
            }
        });
        terminal.show();
        terminal.sendText('echo "🚀 IdliStack Terminal ready! You can now run \\"idlistack init\\", \\"idlistack up\\", \\"idlistack status\\", etc."');
    });

    // Command: idlistack.init
    let initDisposable = vscode.commands.registerCommand("idlistack.init", function () {
        const rootPath = getWorkspaceRoot();
        if (!rootPath) return;
        const terminal = getTerminal();
        terminal.sendText(`cd "${rootPath}"`);
        terminal.sendText(`"${cliPath}" init`);
    });

    // Command: idlistack.inspect (Inspect stack, version, framework & plan preview)
    let inspectDisposable = vscode.commands.registerCommand("idlistack.inspect", function () {
        const rootPath = getWorkspaceRoot();
        if (!rootPath) return;
        const terminal = getTerminal();
        terminal.sendText(`cd "${rootPath}"`);
        terminal.sendText(`"${cliPath}" up --inspect`);
        vscode.window.showInformationMessage('IdliStack: Inspecting stack, runtime version, and framework...');
    });

    // Command: idlistack.up (Deploy to K3s)
    let upDisposable = vscode.commands.registerCommand('idlistack.up', function () {
        const rootPath = getWorkspaceRoot();
        if (!rootPath) return;

        const terminal = getTerminal();
        terminal.sendText(`cd "${rootPath}"`);
        
        const configPath = path.join(rootPath, 'idlistack.toml');
        if (!fs.existsSync(configPath)) {
            terminal.sendText(`"${cliPath}" init && "${cliPath}" up`);
        } else {
            terminal.sendText(`"${cliPath}" up`);
        }
        vscode.window.showInformationMessage('IdliStack: Building OCI image & deploying to K3s...');
    });

    // Command: idlistack.down (Destroy)
    let downDisposable = vscode.commands.registerCommand('idlistack.down', function () {
        const rootPath = getWorkspaceRoot();
        if (!rootPath) return;

        const terminal = getTerminal();
        terminal.sendText(`cd "${rootPath}"`);
        terminal.sendText(`"${cliPath}" down`);
    });

    // Command: idlistack.status
    let statusDisposable = vscode.commands.registerCommand('idlistack.status', function () {
        const rootPath = getWorkspaceRoot();
        if (!rootPath) return;

        const terminal = getTerminal();
        terminal.sendText(`cd "${rootPath}"`);
        terminal.sendText(`"${cliPath}" status`);
    });

    // Command: idlistack.logs
    let logsDisposable = vscode.commands.registerCommand('idlistack.logs', function () {
        const rootPath = getWorkspaceRoot();
        if (!rootPath) return;

        const terminal = getTerminal();
        terminal.sendText(`cd "${rootPath}"`);
        terminal.sendText(`"${cliPath}" logs`);
    });

    // Status bar quick pick menu
    let menuDisposable = vscode.commands.registerCommand('idlistack.menu', async function () {
        const items = [
            { label: '$(cloud-upload) Deploy to K3s', description: 'Detect stack, build OCI image & deploy', cmd: 'idlistack.up' },
            { label: '$(search) Inspect Stack & Plan', description: 'Preview stack, version, framework & build plan', cmd: 'idlistack.inspect' },
            { label: '$(terminal) Open IdliStack Terminal', description: 'Open terminal with idlistack CLI pre-configured', cmd: 'idlistack.terminal' },
            { label: '$(server-environment) Check Status', description: 'Check running pods & service URL', cmd: 'idlistack.status' },
            { label: '$(terminal) View Logs', description: 'Stream container logs from K3s', cmd: 'idlistack.logs' },
            { label: '$(trash) Destroy Deployment', description: 'Uninstall Helm release from K3s', cmd: 'idlistack.down' }
        ];

        const selection = await vscode.window.showQuickPick(items, {
            placeHolder: 'Select an IdliStack action'
        });

        if (selection && selection.cmd) {
            vscode.commands.executeCommand(selection.cmd);
        }
    });

    // Persistent Status Bar Item (Visible immediately on launch)
    const statusBarItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
    statusBarItem.text = '$(rocket) IdliStack';
    statusBarItem.tooltip = 'IdliStack: Zero-Config Deployment on K3s (Click for actions)';
    statusBarItem.command = 'idlistack.menu';
    statusBarItem.show();

    // Welcome Notification on First Install
    const welcomed = context.globalState.get('idlistack.welcomed_v180');
    if (!welcomed) {
        context.globalState.update('idlistack.welcomed_v180', true);
        vscode.window.showInformationMessage(
            'IdliStack v1.8.0 is ready! Terminal commands are now available.',
            'Open Terminal',
            'Deploy to K3s',
            'Inspect Stack'
        ).then(selection => {
            if (selection === 'Open Terminal') vscode.commands.executeCommand('idlistack.terminal');
            if (selection === 'Deploy to K3s') vscode.commands.executeCommand('idlistack.up');
            if (selection === 'Inspect Stack') vscode.commands.executeCommand('idlistack.inspect');
        });
    }

    context.subscriptions.push(
        terminalDisposable,
        initDisposable,
        inspectDisposable,
        upDisposable,
        downDisposable,
        statusDisposable,
        logsDisposable,
        menuDisposable,
        statusBarItem
    );
}

function deactivate() {}

module.exports = {
    activate,
    deactivate
};
