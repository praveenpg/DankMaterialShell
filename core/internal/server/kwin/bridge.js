// DMS prepends `var GENERATION = <n>;` per load. KWin scripts cannot export
// D-Bus methods, so this only pushes state and DMS reloads it to resync.

var SERVICE = "com.danklinux.KWinBridge";
var PATH = "/com/danklinux/KWinBridge";
var IFACE = "com.danklinux.KWinBridge1";
var FLUSH_MS = 50;

var tracked = {};
var dirty = {};
var seq = 0;

function idOf(w) { return w.internalId.toString(); }

function trackable(w) {
    return w && w.managed && !w.deleted && (w.normalWindow || w.dialog);
}

function windowRecord(w) {
    return {
        id: idOf(w),
        appId: w.desktopFileName || w.resourceClass,
        title: w.caption,
        desktops: w.desktops.map(function (d) { return d.id; }),
        onAllDesktops: w.onAllDesktops,
        output: w.output ? w.output.name : "",
        active: w.active,
        minimized: w.minimized,
        demandsAttention: w.demandsAttention,
        skipTaskbar: w.skipTaskbar
    };
}

function desktopRecords() {
    return workspace.desktops.map(function (d) { return { id: d.id, name: d.name }; });
}

function outputRecords() {
    return workspace.screens.map(function (o) {
        var d = workspace.currentDesktopForScreen(o);
        return { name: o.name, currentDesktop: d ? d.id : "" };
    });
}

// DMS's D-Bus library handles each call on its own goroutine, so ordering travels in seq.
function emit(type, fields) {
    fields.type = type;
    fields.generation = GENERATION;
    fields.seq = ++seq;
    callDBus(SERVICE, PATH, IFACE, "Event", JSON.stringify(fields));
}

var flushTimer = new QTimer();
flushTimer.singleShot = true;
flushTimer.interval = FLUSH_MS;
flushTimer.timeout.connect(function () {
    var windows = [];
    for (var id in dirty) {
        if (tracked[id])
            windows.push(windowRecord(tracked[id]));
    }
    dirty = {};
    if (windows.length)
        emit("changed", { windows: windows });
});

var WATCHED = ["captionChanged", "minimizedChanged", "demandsAttentionChanged", "skipTaskbarChanged",
    "desktopsChanged", "outputChanged", "desktopFileNameChanged", "windowClassChanged"];

function track(w) {
    if (!trackable(w))
        return false;
    var id = idOf(w);
    if (tracked[id])
        return false;
    tracked[id] = w;
    WATCHED.forEach(function (sig) {
        w[sig].connect(function () {
            if (!tracked[id])
                return;
            dirty[id] = true;
            if (!flushTimer.active)
                flushTimer.start();
        });
    });
    return true;
}

workspace.windowAdded.connect(function (w) {
    if (track(w))
        emit("added", { window: windowRecord(w) });
});

workspace.windowRemoved.connect(function (w) {
    var id = idOf(w);
    if (!tracked[id])
        return;
    delete tracked[id];
    delete dirty[id];
    emit("removed", { id: id });
});

// KWin activates a new window before windowAdded fires.
workspace.windowActivated.connect(function (w) {
    if (w && track(w))
        emit("added", { window: windowRecord(w) });
    emit("active", { id: w && tracked[idOf(w)] ? idOf(w) : "" });
});

// One call per affected output: every output in global mode. D-Bus stays
// silent when a non-active output switches in per-output mode.
workspace.currentDesktopChanged.connect(function (previous, current, output) {
    emit("desktopChanged", { output: output ? output.name : "", current: current ? current.id : "" });
});

var named = {};

function watchDesktopNames() {
    workspace.desktops.forEach(function (d) {
        if (named[d.id])
            return;
        named[d.id] = true;
        d.nameChanged.connect(function () { emit("desktopsChanged", { desktops: desktopRecords() }); });
        d.aboutToBeDestroyed.connect(function () { delete named[d.id]; });
    });
}

workspace.desktopsChanged.connect(function () {
    watchDesktopNames();
    emit("desktopsChanged", { desktops: desktopRecords() });
});

workspace.screensChanged.connect(function () {
    emit("outputsChanged", { outputs: outputRecords() });
});

options.perOutputVirtualDesktopsChanged.connect(function () {
    emit("modeChanged", { perOutputDesktops: options.perOutputVirtualDesktops, outputs: outputRecords() });
});

workspace.windowList().forEach(track);
watchDesktopNames();
emit("snapshot", {
    perOutputDesktops: options.perOutputVirtualDesktops,
    desktops: desktopRecords(),
    outputs: outputRecords(),
    windows: Object.keys(tracked).map(function (id) { return windowRecord(tracked[id]); })
});
