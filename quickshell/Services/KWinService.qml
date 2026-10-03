pragma Singleton
pragma ComponentBehavior: Bound

import QtQuick
import Quickshell
import qs.Common
import qs.Services

Singleton {
    id: root
    readonly property var log: Log.scoped("KWinService")

    property var state: ({})
    readonly property bool available: state.available === true
    readonly property var desktops: state.desktops ?? []
    readonly property var outputs: state.outputs ?? []
    readonly property var windows: state.windows ?? []
    readonly property string focusedOutput: windows.find(w => w.active)?.output ?? ""
    readonly property var toplevels: windows.filter(w => !w.skipTaskbar).map(w => windowFacade(w))
    readonly property var focusedWindow: {
        const w = windows.find(w => w.active && !w.skipTaskbar);
        return w ? windowFacade(w) : null;
    }
    readonly property var focusedDesktop: {
        const output = outputs.find(o => o.name === focusedOutput) ?? outputs[0];
        return desktops.find(d => d.id === output?.currentDesktop) ?? null;
    }

    Connections {
        target: DMSService
        function onKwinStateUpdate(data) {
            root.state = data;
        }
    }

    function switchDesktop(desktop, output) {
        DMSService.sendRequest("kwin.switchDesktop", {
            "desktop": desktop,
            "output": output ?? ""
        }, response => {
            if (response.error)
                log.warn("switch desktop failed:", response.error);
        });
    }

    function windowAction(window, action, extra) {
        DMSService.sendRequest("kwin.windowAction", Object.assign({
            "window": window,
            "action": action
        }, extra ?? {}), response => {
            if (response.error)
                log.warn("window", action, "failed:", response.error);
        });
    }

    function windowFacade(w) {
        return {
            kwinWindowId: w.id,
            kwinKey: "kwin:" + w.id,
            kwinOutput: w.output,
            kwinDesktops: w.desktops,
            kwinOnAllDesktops: w.onAllDesktops,
            appId: w.appId,
            title: w.title,
            activated: w.active,
            pid: w.pid,
            canMinimize: w.canMinimize,
            canMaximize: w.canMaximize,
            canClose: w.canClose,
            screens: Quickshell.screens.filter(s => s.name === w.output),
            get minimized() {
                return w.minimized;
            },
            set minimized(value) {
                root.windowAction(w.id, "setMinimized", {
                    "value": value
                });
            },
            get maximized() {
                return w.maximized;
            },
            set maximized(value) {
                root.windowAction(w.id, "setMaximized", {
                    "value": value
                });
            },
            get fullscreen() {
                return w.fullscreen;
            },
            set fullscreen(value) {
                root.windowAction(w.id, "setFullscreen", {
                    "value": value
                });
            },
            activate: () => root.windowAction(w.id, "activate"),
            close: () => root.windowAction(w.id, "close")
        };
    }

    function renameDesktop(desktop, name) {
        DMSService.sendRequest("kwin.renameDesktop", {
            "desktop": desktop,
            "name": name
        }, response => {
            if (response.error)
                log.warn("rename desktop failed:", response.error);
        });
    }
}
