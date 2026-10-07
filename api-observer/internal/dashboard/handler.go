// Package dashboard handles all the frontend calls
package dashboard

import (
	"api-observer/internal/audit"
	"api-observer/internal/config"
	"api-observer/internal/nodes"
	"net/http"
)

type Handler struct {
	RuleSet *audit.RuleSet
	Nodes   *nodes.NodeManager
	Config  *config.Config
	Queue   *audit.Queue
}

func NewHandler(rs *audit.RuleSet, nm *nodes.NodeManager, cfg *config.Config, queue *audit.Queue) *Handler {
	return &Handler{
		RuleSet: rs,
		Nodes:   nm,
		Config:  cfg,
		Queue:   queue,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /", h.ExplorerPage)
	mux.HandleFunc("GET /logs", h.ExplorerLogs)

	mux.HandleFunc("GET /rules", h.RulesPage)
	mux.HandleFunc("GET /rules/import", h.RulesImportPage)
	mux.HandleFunc("GET /rules/import/translate", h.RulesImportTranslate)

	mux.HandleFunc("GET /nodes", h.NodesPage)
	mux.HandleFunc("GET /nodes/list", h.NodesList)

	mux.HandleFunc("POST /nodes", h.AddNode)
	mux.HandleFunc("DELETE /nodes", h.DeleteNode)
}
