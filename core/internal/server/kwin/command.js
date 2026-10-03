// Loaded once per command with `run(<command json>);` appended, so nothing runs at idle.

function findDesktop(id) {
    return workspace.desktops.filter(function (d) { return d.id === id; })[0];
}

function findOutput(name) {
    return workspace.screens.filter(function (o) { return o.name === name; })[0];
}

function findWindow(id) {
    return workspace.windowList().filter(function (w) { return w.internalId.toString() === id; })[0];
}

function requireBool(cmd) {
    if (typeof cmd.value !== "boolean")
        throw "value must be a boolean";
    return cmd.value;
}

// Window ops take explicit values; KWin's own toggles race with DMS's view of the state.
var WINDOW_HANDLERS = {
    activate: function (w) {
        // Assigning activeWindow does not restore a minimized window.
        w.minimized = false;
        workspace.activeWindow = w;
    },
    close: function (w) {
        if (!w.closeable)
            throw "window cannot be closed";
        w.closeWindow();
    },
    setMinimized: function (w, cmd) { w.minimized = requireBool(cmd); },
    setMaximized: function (w, cmd) { var v = requireBool(cmd); w.setMaximize(v, v); },
    setFullscreen: function (w, cmd) { w.fullScreen = requireBool(cmd); },
    moveToDesktop: function (w, cmd) {
        var d = findDesktop(cmd.desktop);
        if (!d)
            throw "unknown desktop " + cmd.desktop;
        w.desktops = [d];
    },
    moveToOutput: function (w, cmd) {
        var o = findOutput(cmd.output);
        if (!o)
            throw "unknown output " + cmd.output;
        workspace.sendClientToScreen(w, o);
    }
};

var HANDLERS = {
    window: function (cmd) {
        var handler = WINDOW_HANDLERS[cmd.action];
        if (!handler)
            throw "unknown window action " + cmd.action;
        var w = findWindow(cmd.window);
        if (!w)
            throw "unknown window " + cmd.window;
        handler(w, cmd);
    },
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
