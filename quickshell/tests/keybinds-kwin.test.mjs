import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import test from "node:test";

const source = readFileSync(new URL("../Services/KeybindsService.qml", import.meta.url), "utf8");

function binding(name) {
    const match = source.match(new RegExp(`^    (?:readonly )?property (?:bool|string) ${name}: ([^\\n{]+|\\{[\\s\\S]*?\\n    \\})$`, "m"));
    assert.ok(match, name);
    return match[1].startsWith("{") ? `(() => ${match[1]})()` : `(${match[1]})`;
}

const FLAGS = ["isAqueous", "isNiri", "isHyprland", "isMango", "isKwin"];

function evaluate(flag, hyprlandConfReadOnly = false) {
    const scope = { CompositorService: Object.fromEntries(FLAGS.map(f => [f, f === flag])), dmsStatus: { readOnly: hyprlandConfReadOnly } };
    for (const name of ["available", "currentProvider", "cheatsheetProvider"])
        scope[name] = vm.runInNewContext(binding(name), scope);
    scope.hyprlandLegacy = vm.runInNewContext(binding("hyprlandLegacy"), scope);
    scope.readOnly = vm.runInNewContext(binding("readOnly"), scope);
    return scope;
}

test("kwin shortcuts come from the kwin provider and are read-only in DMS", () => {
    const kwin = evaluate("isKwin");
    assert.equal(kwin.available, true);
    assert.equal(kwin.currentProvider, "kwin");
    assert.equal(kwin.cheatsheetProvider, "kwin");
    assert.equal(kwin.readOnly, true);
    assert.equal(kwin.hyprlandLegacy, false);
});

test("only hyprland conf mode is read-only among editable providers", () => {
    for (const flag of ["isNiri", "isMango", "isAqueous", "isHyprland"])
        assert.equal(evaluate(flag).readOnly, false, flag);
    const legacy = evaluate("isHyprland", true);
    assert.equal(legacy.readOnly, true);
    assert.equal(legacy.hyprlandLegacy, true);
});
