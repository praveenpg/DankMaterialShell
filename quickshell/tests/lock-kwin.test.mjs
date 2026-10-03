import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import test from "node:test";

const source = readFileSync(new URL("../Modules/Lock/Lock.qml", import.meta.url), "utf8");

function method(name) {
    const start = source.indexOf("    function " + name + "(");
    return source.slice(start, source.indexOf("\n    }", start) + 6);
}

function lockOn(isKwin) {
    const calls = [];
    const context = vm.createContext({
        SettingsData: { customPowerActionLock: "", loginctlLockIntegration: false },
        CompositorService: { isKwin, powerOffMonitors: () => calls.push("powerOff") },
        KWinService: { lock: () => calls.push("kwinLock") },
        SessionService: { active: true, loginctlAvailable: false },
        IdleService: {},
        sessionLock: { secure: false },
        shouldLock: false,
        pendingLock: false,
        powerOffOnLock: false,
        resetLockRetry() {},
        notifyLoginctl() {},
        spawnCustomLocker() { calls.push("custom"); },
        beginPowerOff() { calls.push("beginPowerOff"); }
    });
    for (const name of ["lock", "lockAndOutputsOff"])
        vm.runInContext(method(name), context);
    return { context, calls };
}

test("on kwin, lock hands over to kwin's locker instead of requesting a session lock", () => {
    const kwin = lockOn(true);
    kwin.context.lock();
    assert.deepEqual(kwin.calls, ["kwinLock"]);
    assert.equal(kwin.context.shouldLock, false);

    const other = lockOn(false);
    other.context.lock();
    assert.deepEqual(other.calls, []);
    assert.equal(other.context.shouldLock, true);
});

test("on kwin, lock and outputs off locks first, then powers the outputs off", () => {
    const kwin = lockOn(true);
    kwin.context.lockAndOutputsOff();
    assert.deepEqual(kwin.calls, ["kwinLock", "powerOff"]);
});
