import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import test from "node:test";

const source = readFileSync(new URL("../Services/SessionService.qml", import.meta.url), "utf8");

function method(name) {
    const start = source.indexOf("    function " + name + "(");
    return source.slice(start, source.indexOf("\n    }", start) + 6);
}

const FLAGS = ["isAqueous", "isNiri", "isMango", "isLabwc", "isUmbriel", "isSway", "isScroll", "isMiracle", "isHyprland", "isKwin"];

function logoutOn(flag) {
    const calls = [];
    const record = name => () => calls.push(name);
    const context = vm.createContext({
        SettingsData: { customPowerActionLogout: "" },
        CompositorService: Object.fromEntries(FLAGS.map(f => [f, f === flag])),
        AqueousService: { quit: record("aqueous") },
        NiriService: { quit: record("niri") },
        MangoService: { quit: record("mango") },
        LabwcService: { quit: record("labwc") },
        UmbrielService: { action: record("umbriel") },
        I3: { dispatch: record("i3") },
        HyprlandService: { exit: record("hyprland") },
        KWinService: { logout: record("kwin") },
        Quickshell: { execDetached: record("custom") }
    });
    vm.runInContext(method("_logout"), context);
    context._logout();
    return calls;
}

test("logout asks the running compositor to quit, never another one", () => {
    const expected = { isAqueous: "aqueous", isNiri: "niri", isMango: "mango", isLabwc: "labwc", isUmbriel: "umbriel", isSway: "i3", isScroll: "i3", isMiracle: "i3", isHyprland: "hyprland", isKwin: "kwin" };
    for (const [flag, call] of Object.entries(expected))
        assert.deepEqual(logoutOn(flag), [call], flag);
});
