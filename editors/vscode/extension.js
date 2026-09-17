const vscode = require('vscode');
const { execFile } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');

function activate(context) {
  const item = vscode.window.createStatusBarItem('gitplex.ownership', vscode.StatusBarAlignment.Left, 20);
  item.name = 'Gitplex File Ownership';
  item.command = 'gitplex.showOwnership';
  const output = vscode.window.createOutputChannel('Gitplex Ownership');
  let revision = 0;
  let child;
  let details;
  let disposed = false;
  function update() {
    const current = ++revision;
    if (child) { child.kill(); child = undefined; }
    details = undefined;
    item.hide();
    const document = vscode.window.activeTextEditor?.document;
    if (!vscode.workspace.isTrusted || !document || document.uri.scheme !== 'file') return;
    let root = path.dirname(document.uri.fsPath);
    while (!fs.existsSync(path.join(root, '.gitplex', 'state.json'))) {
      const parent = path.dirname(root);
      if (root === parent) return;
      root = parent;
    }
    const binary = vscode.workspace.getConfiguration('gitplex').get('executablePath', 'gitplex');
    child = execFile(binary, ['which', '--json', document.uri.fsPath], { cwd: root, timeout: 5000, maxBuffer: 1024 * 1024 }, (error, stdout, stderr) => {
      if (disposed || current !== revision) return;
      child = undefined;
      if (error) {
        item.text = '$(info) Gitplex: lookup unavailable';
        details = `Ownership lookup failed. Check gitplex.executablePath and ensure the binary supports which --json.\n${stderr || error.message}`;
      } else {
        try {
          const owner = JSON.parse(stdout);
          item.text = `$(repo) ${owner.repo || 'Gitplex'} · ${owner.kind} · ${owner.publishable ? 'publishable' : 'not publishable'}`;
          details = `Workspace: ${owner.workspace_path}\nSource: ${owner.repo ? `${owner.repo}:${owner.path}` : 'none'}\nOriginal: ${owner.source_path || 'none'}\nGenerated: ${owner.generated}\nCopied: ${owner.copied}\nPublishable: ${owner.publishable}`;
        } catch (error) {
          item.text = '$(info) Gitplex: invalid lookup output';
          details = error.message;
        }
      }
      item.tooltip = details;
      item.show();
    });
  }
  const watcher = vscode.workspace.createFileSystemWatcher('**/.gitplex/{state.json,manifest.yaml}');
  context.subscriptions.push(item, output, watcher,
    vscode.commands.registerCommand('gitplex.showOwnership', () => { if (details) { output.clear(); output.appendLine(details); output.show(true); } }),
    vscode.window.onDidChangeActiveTextEditor(update),
    vscode.workspace.onDidSaveTextDocument(update),
    vscode.workspace.onDidChangeConfiguration(event => { if (event.affectsConfiguration('gitplex')) update(); }),
    watcher.onDidChange(update), watcher.onDidCreate(update), watcher.onDidDelete(update),
    { dispose() { disposed = true; ++revision; if (child) child.kill(); } }
  );
  update();
}
module.exports = { activate };
