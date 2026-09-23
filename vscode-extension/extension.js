const vscode = require('vscode');
const path = require('path');
const fs = require('fs');

/**
 * @param {vscode.ExtensionContext} context
 */
function activate(context) {
    console.log('IdliStack extension is now active!');

    // Path to the bundled CLI binary
    const cliPath = path.join(context.extensionPath, 'bin', 'idlistack');
    
    // Ensure it is executable
    try {
        if (fs.existsSync(cliPath)) {
            fs.chmodSync(cliPath, 0o755);
        }
    } catch (e) {
        console.error('Failed to set execute permissions on CLI binary', e);
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

    // Persistent Status Bar Item
    const statusBarItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
    statusBarItem.text = '$(rocket) IdliStack';
    statusBarItem.tooltip = 'IdliStack: Zero-Config Deployment on K3s (Click for actions)';
    statusBarItem.command = 'idlistack.menu';
    statusBarItem.show();

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
