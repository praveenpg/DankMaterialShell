package kwin

import (
	"fmt"

	"github.com/AvengeMedia/DankMaterialShell/core/internal/server/models"
	"github.com/AvengeMedia/dankgo/ipc"
	"github.com/AvengeMedia/dankgo/ipc/params"
)

func HandleRequest(conn *ipc.ConnWriter, req ipc.Request, manager *Manager) {
	if manager == nil {
		models.RespondError(conn, req.ID, "kwin manager not initialized")
		return
	}

	switch req.Method {
	case "kwin.getState":
		models.Respond(conn, req.ID, manager.GetState())
	case "kwin.switchDesktop":
		handleSwitchDesktop(conn, req, manager)
	case "kwin.renameDesktop":
		handleRenameDesktop(conn, req, manager)
	case "kwin.windowAction":
		handleWindowAction(conn, req, manager)
	case "kwin.lock":
		if err := manager.Lock(); err != nil {
			models.RespondError(conn, req.ID, err.Error())
			return
		}
		models.Respond(conn, req.ID, models.SuccessResult{Success: true, Message: "locked"})
	case "kwin.logout":
		if err := manager.Logout(); err != nil {
			models.RespondError(conn, req.ID, err.Error())
			return
		}
		models.Respond(conn, req.ID, models.SuccessResult{Success: true, Message: "logging out"})
	default:
		models.RespondError(conn, req.ID, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

func handleSwitchDesktop(conn *ipc.ConnWriter, req ipc.Request, manager *Manager) {
	desktop, err := params.StringNonEmpty(req.Params, "desktop")
	if err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}
	if err := manager.SwitchDesktop(desktop, params.StringOpt(req.Params, "output", "")); err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}
	models.Respond(conn, req.ID, models.SuccessResult{Success: true, Message: "desktop switched"})
}

func handleRenameDesktop(conn *ipc.ConnWriter, req ipc.Request, manager *Manager) {
	desktop, err := params.StringNonEmpty(req.Params, "desktop")
	if err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}
	name, err := params.StringNonEmpty(req.Params, "name")
	if err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}
	if err := manager.RenameDesktop(desktop, name); err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}
	models.Respond(conn, req.ID, models.SuccessResult{Success: true, Message: "desktop renamed"})
}

func handleWindowAction(conn *ipc.ConnWriter, req ipc.Request, manager *Manager) {
	window, err := params.StringNonEmpty(req.Params, "window")
	if err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}
	action, err := params.StringNonEmpty(req.Params, "action")
	if err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}
	args := map[string]any{}
	for _, key := range []string{"value", "desktop", "output"} {
		if v, ok := params.Any(req.Params, key); ok {
			args[key] = v
		}
	}
	if err := manager.WindowAction(window, action, args); err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}
	models.Respond(conn, req.ID, models.SuccessResult{Success: true, Message: "window action applied"})
}
