package providers

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AvengeMedia/DankMaterialShell/core/internal/keybinds"
)

// KWinProvider lists KDE global shortcuts read-only. kglobalaccel keeps them in
// kglobalshortcutsrc with keys already in text form; editing stays with KDE's settings.
type KWinProvider struct {
	path string
}

func NewKWinProvider(path string) *KWinProvider {
	if path == "" {
		if dir, err := os.UserConfigDir(); err == nil {
			path = filepath.Join(dir, "kglobalshortcutsrc")
		}
	}
	return &KWinProvider{path: path}
}

func (*KWinProvider) Name() string { return "kwin" }

func (*KWinProvider) ModKey() keybinds.ModKey { return keybinds.DefaultModKey() }

func (k *KWinProvider) GetCheatSheet() (*keybinds.CheatSheet, error) {
	sheet := &keybinds.CheatSheet{Title: "KWin", Provider: "kwin", DMSBindsIncluded: true, Binds: map[string][]keybinds.Keybind{}}
	sheet.SetMod(k.ModKey())
	f, err := os.Open(k.path)
	if os.IsNotExist(err) {
		return sheet, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", k.path, err)
	}
	defer f.Close()
	binds, err := parseKglobalshortcuts(f)
	if err != nil {
		return nil, err
	}
	sheet.Binds = binds
	return sheet, nil
}

// parseKglobalshortcuts reads entries shaped `action=current keys,default keys,Friendly name`;
// keys are tab-escaped lists and "none" means unbound, which is left out of the list.
func parseKglobalshortcuts(r io.Reader) (map[string][]keybinds.Keybind, error) {
	type entry struct{ action, keys, desc string }
	groups := map[string][]entry{}
	names := map[string]string{}
	var order []string
	group := ""

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			group = line[1 : len(line)-1]
			if _, seen := groups[group]; !seen {
				groups[group] = nil
				order = append(order, group)
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || group == "" {
			continue
		}
		if key == "_k_friendly_name" {
			names[group] = value
			continue
		}
		fields := strings.SplitN(value, ",", 3)
		desc := key
		if len(fields) == 3 && fields[2] != "" {
			desc = fields[2]
		}
		groups[group] = append(groups[group], entry{action: key, keys: fields[0], desc: desc})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse kglobalshortcutsrc: %w", err)
	}

	binds := map[string][]keybinds.Keybind{}
	for _, g := range order {
		// DMS replaces plasmashell, so its shortcuts do nothing in a DMS session.
		if g == "plasmashell" {
			continue
		}
		category := names[g]
		if strings.HasPrefix(g, "services][") {
			category = "Applications"
		}
		if category == "" {
			category = g
		}
		for _, e := range groups[g] {
			for _, key := range strings.Split(e.keys, `\t`) {
				key = strings.TrimSpace(key)
				if key == "" || strings.EqualFold(key, "none") {
					continue
				}
				desc := e.desc
				if category == "Applications" && desc == e.action {
					desc = strings.TrimSuffix(strings.TrimPrefix(g, "services]["), ".desktop") + " " + e.action
				}
				binds[category] = append(binds[category], keybinds.Keybind{
					Key:         key,
					Description: desc,
					Action:      g + "/" + e.action,
					Source:      "kglobalaccel",
				})
			}
		}
	}
	return binds, nil
}
