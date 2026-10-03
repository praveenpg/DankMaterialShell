package kwin

type Desktop struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Output struct {
	Name           string `json:"name"`
	CurrentDesktop string `json:"currentDesktop"`
}

type Window struct {
	ID               string   `json:"id"`
	AppID            string   `json:"appId"`
	Title            string   `json:"title"`
	Desktops         []string `json:"desktops"`
	OnAllDesktops    bool     `json:"onAllDesktops"`
	Output           string   `json:"output"`
	Active           bool     `json:"active"`
	Minimized        bool     `json:"minimized"`
	Maximized        bool     `json:"maximized"`
	Fullscreen       bool     `json:"fullscreen"`
	DemandsAttention bool     `json:"demandsAttention"`
	SkipTaskbar      bool     `json:"skipTaskbar"`
	CanClose         bool     `json:"canClose"`
	CanMinimize      bool     `json:"canMinimize"`
	CanMaximize      bool     `json:"canMaximize"`
	PID              int      `json:"pid"`
}

type State struct {
	Available         bool      `json:"available"`
	PerOutputDesktops bool      `json:"perOutputDesktops"`
	Desktops          []Desktop `json:"desktops"`
	Outputs           []Output  `json:"outputs"`
	Windows           []Window  `json:"windows"`
}

type event struct {
	Generation        uint64    `json:"generation"`
	Seq               uint64    `json:"seq"`
	Type              string    `json:"type"`
	PerOutputDesktops bool      `json:"perOutputDesktops"`
	Desktops          []Desktop `json:"desktops"`
	Outputs           []Output  `json:"outputs"`
	Windows           []Window  `json:"windows"`
	Window            *Window   `json:"window"`
	ID                string    `json:"id"`
	Output            string    `json:"output"`
	Current           string    `json:"current"`
}

type commandResult struct {
	ID    uint64 `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}
