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
