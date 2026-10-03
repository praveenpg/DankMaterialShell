// Loaded once per command with `run(<command json>);` appended, so nothing runs at idle.

function findDesktop(id) {
    return workspace.desktops.filter(function (d) { return d.id === id; })[0];
}

function findOutput(name) {
    return workspace.screens.filter(function (o) { return o.name === name; })[0];
}

var HANDLERS = {
    switchDesktop: function (cmd) {
        var d = findDesktop(cmd.desktop);
        if (!d)
            throw "unknown desktop " + cmd.desktop;
        if (!cmd.output) {
            workspace.currentDesktop = d;
            return;
        }
        var o = findOutput(cmd.output);
        if (!o)
            throw "unknown output " + cmd.output;
        workspace.setCurrentDesktopForScreen(d, o);
    }
};

function run(cmd) {
    var result = { id: cmd.id, ok: true };
    try {
        var handler = HANDLERS[cmd.op];
        if (!handler)
            throw "unknown op " + cmd.op;
        handler(cmd);
    } catch (e) {
        result.ok = false;
        result.error = String(e);
    }
    callDBus("com.danklinux.KWinBridge", "/com/danklinux/KWinBridge", "com.danklinux.KWinBridge1",
        "Result", JSON.stringify(result));
}
