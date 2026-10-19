// Package dashboard handles all the frontend calls
package dashboard

import (
	"api-observer/internal/audit"
	"api-observer/internal/config"
	"api-observer/internal/nodes"
	"database/sql"

	"net/http"
	"path/filepath"
)

type Handler struct {
	RuleSet *audit.RuleSet
	Nodes   *nodes.NodeManager
	Config  *config.Config
	Queue   *audit.Queue
	DB      *sql.DB

	FindingsFilePath string
	NodeIndexDir     string
}

func NewHandler(
	rs *audit.RuleSet,
	nm *nodes.NodeManager,
	cfg *config.Config,
	queue *audit.Queue,
	db *sql.DB,
) *Handler {
	return &Handler{
		RuleSet: rs,
		Nodes:   nm,
		Config:  cfg,
		Queue:   queue,
		DB:      db,

		FindingsFilePath: cfg.FindingsLog,
		NodeIndexDir: filepath.Dir(
			cfg.FindingsLog,
		),
	}
}

func (h *Handler) RegisterRoutes(
	mux *http.ServeMux,
) {
	mux.HandleFunc(
		"GET /",
		h.ExplorerPage,
	)

	mux.HandleFunc(
		"GET /logs",
		h.ExplorerLogs,
	)

	mux.HandleFunc(
		"GET /analysis",
		h.AnalysisPage,
	)

	mux.HandleFunc(
		"POST /analysis/logs",
		h.AnalysisLogs,
	)

	mux.HandleFunc(
		"GET /rules",
		h.RulesPage,
	)

	mux.HandleFunc(
		"GET /rules/import",
		h.RulesImportPage,
	)

	mux.HandleFunc(
		"POST /rules/import/translate",
		h.RulesImportTranslate,
	)

	mux.HandleFunc(
		"GET /nodes",
		h.NodesPage,
	)

	mux.HandleFunc(
		"GET /nodes/list",
		h.NodesList,
	)

	mux.HandleFunc(
		"POST /nodes/name",
		h.SetNodeName,
	)
}
