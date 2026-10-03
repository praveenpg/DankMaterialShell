package providers

import (
	"path/filepath"
	"testing"
)

func TestKWinProviderListsBoundShortcutsByComponent(t *testing.T) {
	sheet, err := NewKWinProvider("testdata/kglobalshortcutsrc").GetCheatSheet()
	if err != nil {
		t.Fatal(err)
	}
	keys := func(category string) map[string]string {
		got := map[string]string{}
		for _, b := range sheet.Binds[category] {
			got[b.Key] = b.Description
		}
		return got
	}

	kwin := keys("KWin")
	want := map[string]string{
		"Ctrl+F9":   "Toggle Present Windows (Current desktop)",
		"Meta+F9":   "Toggle Present Windows (Current desktop)",
		"Alt+F4":    "Close Window",
		"Meta+PgUp": "Maximize Window",
	}
	if len(kwin) != len(want) {
		t.Fatalf("KWin binds = %v, want %v", kwin, want)
	}
	for k, v := range want {
		if kwin[k] != v {
			t.Errorf("KWin %q = %q, want %q", k, kwin[k], v)
		}
	}
	if got := keys("Session Management"); got["Meta+L"] != "Lock Session" || got["Screensaver"] != "Lock Session" {
		t.Errorf("Session Management = %v", got)
	}
	if got := keys("Applications"); got["Ctrl+Alt+T"] != "org.kde.konsole _launch" || got["Meta+Shift+S"] != "Capture Rectangular Region" || len(got) != 2 {
		t.Errorf("Applications = %v", got)
	}
	if _, ok := sheet.Binds["plasmashell"]; ok {
		t.Error("plasmashell shortcuts must be left out; DMS replaces plasmashell")
	}
	if _, ok := sheet.Binds["Activity Manager"]; ok {
		t.Error("a component with only unbound actions must not appear")
	}
	if !sheet.DMSBindsIncluded {
		t.Error("read-only provider must not ask for a DMS binds include")
	}
}

func TestKWinProviderWithoutAShortcutsFileIsEmptyNotAnError(t *testing.T) {
	sheet, err := NewKWinProvider(filepath.Join(t.TempDir(), "missing")).GetCheatSheet()
	if err != nil || len(sheet.Binds) != 0 {
		t.Fatalf("sheet = %+v, err = %v; want an empty sheet", sheet, err)
	}
}
