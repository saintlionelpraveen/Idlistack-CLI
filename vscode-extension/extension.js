const vscode = require('vscode');
const path = require('path');
const fs = require('fs');
const os = require('os');

/**
 * @param {vscode.ExtensionContext} context
 */
function activate(context) {
    console.log('IdliStack extension is now active!');

    // Path to the bundled CLI binary directory
    const binDir = path.join(context.extensionPath, 'bin');
    const cliPath = path.join(binDir, 'idlistack');
    
    // 1. Ensure binary is executable (chmod +x)
    try {
        if (fs.existsSync(cliPath)) {
            fs.chmodSync(cliPath, 0o755);
        }
    } catch (e) {
        console.error('Failed to set execute permissions on CLI binary', e);
    }

    // 2. Automatically inject the extension bin directory into VS Code integrated terminals' PATH!
    // This allows users to open any terminal inside VS Code and directly run `idlistack <command>`.
    if (context.environmentVariableCollection) {
        context.environmentVariableCollection.prepend('PATH', `${binDir}${path.delimiter}`);
        context.environmentVariableCollection.description = 'IdliStack CLI binary path';
    }

    // 3. Symlink / copy to ~/.local/bin/idlistack for system-wide terminal access outside VS Code
    try {
        const localBin = path.join(os.homedir(), '.local', 'bin');
        if (!fs.existsSync(localBin)) {
            fs.mkdirSync(localBin, { recursive: true });
        }
        const userSymlink = path.join(localBin, 'idlistack');
        if (!fs.existsSync(userSymlink)) {
            try {
                fs.symlinkSync(cliPath, userSymlink);
            } catch (err) {
                // If symlink fails (e.g. cross-filesystem), copy binary
                fs.copyFileSync(cliPath, userSymlink);
                fs.chmodSync(userSymlink, 0o755);
            }
        }
    } catch (e) {
        console.error('Failed to register ~/.local/bin/idlistack', e);
    }

    // Helper to get or create a terminal
    function getTerminal() {
        let terminal = vscode.window.terminals.find(t => t.name === 'IdliStack');
        if (!terminal) {
            terminal = vscode.window.createTerminal('IdliStack');
        }
        terminal.show();
        return terminal;
    }

    function getWorkspaceRoot() {
        const workspaceFolders = vscode.workspace.workspaceFolders;
        if (!workspaceFolders || workspaceFolders.length === 0) {
            vscode.window.showErrorMessage('Please open a workspace folder to use IdliStack.');
            return null;
        }
        return workspaceFolders[0].uri.fsPath;
    }

    // Command: idlistack init
    let initDisposable = vscode.commands.registerCommand("idlistack.init", function () {
        const rootPath = getWorkspaceRoot();
        if (!rootPath) return;
        const terminal = getTerminal();
        terminal.sendText(`cd "${rootPath}"`);
        terminal.sendText(`"${cliPath}" init`);
    });

    // Command: idlistack inspect (Auto-detect stack, version, framework & build plan preview)
    let inspectDisposable = vscode.commands.registerCommand("idlistack.inspect", function () {
        const rootPath = getWorkspaceRoot();
        if (!rootPath) return;
        const terminal = getTerminal();
        terminal.sendText(`cd "${rootPath}"`);
        terminal.sendText(`"${cliPath}" up --inspect`);
        vscode.window.showInformationMessage('IdliStack: Inspecting stack, runtime version, and framework...');
    });

    // Command: idlistack up (Deploy to K3s)
    let upDisposable = vscode.commands.registerCommand('idlistack.up', function () {
        const rootPath = getWorkspaceRoot();
        if (!rootPath) return;

        const terminal = getTerminal();
        terminal.sendText(`cd "${rootPath}"`);
        
        // If idlistack.toml doesn't exist, init first
        const configPath = path.join(rootPath, 'idlistack.toml');
        if (!fs.existsSync(configPath)) {
            terminal.sendText(`"${cliPath}" init && "${cliPath}" up`);
        } else {
            terminal.sendText(`"${cliPath}" up`);
        }
        vscode.window.showInformationMessage('IdliStack: Building OCI image & deploying to K3s...');
    });

    // Command: idlistack down (Destroy)
    let downDisposable = vscode.commands.registerCommand('idlistack.down', function () {
        const rootPath = getWorkspaceRoot();
        if (!rootPath) return;

        const terminal = getTerminal();
        terminal.sendText(`cd "${rootPath}"`);
        terminal.sendText(`"${cliPath}" down`);
    });

    // Command: idlistack status
    let statusDisposable = vscode.commands.registerCommand('idlistack.status', function () {
        const rootPath = getWorkspaceRoot();
        if (!rootPath) return;

        const terminal = getTerminal();
        terminal.sendText(`cd "${rootPath}"`);
        terminal.sendText(`"${cliPath}" status`);
    });

    // Command: idlistack logs
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

    // Persistent Status Bar Item (Visible on startup)
    const statusBarItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
    statusBarItem.text = '$(rocket) IdliStack';
    statusBarItem.tooltip = 'IdliStack: Zero-Config Deployment on K3s (Click for actions)';
    statusBarItem.command = 'idlistack.menu';
    statusBarItem.show();

    // Welcome notification on first install
    const welcomed = context.globalState.get('idlistack.welcomed_v1');
    if (!welcomed) {
        context.globalState.update('idlistack.welcomed_v1', true);
        vscode.window.showInformationMessage(
            'IdliStack is ready! Click $(rocket) IdliStack in the status bar or type "idlistack" in any new terminal.',
            'Deploy to K3s',
            'Inspect Stack'
        ).then(selection => {
            if (selection === 'Deploy to K3s') vscode.commands.executeCommand('idlistack.up');
            if (selection === 'Inspect Stack') vscode.commands.executeCommand('idlistack.inspect');
        });
    }

    context.subscriptions.push(
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
