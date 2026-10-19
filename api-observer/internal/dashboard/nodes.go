package dashboard

import (
	"fmt"
	"net/http"
	"strings"

	nodespage "api-observer/internal/dashboard/views/nodes-page"
)

func (h *Handler) NodesPage(
	w http.ResponseWriter,
	r *http.Request,
) {
	if err := nodespage.NodesPage(
		"Nodes",
		h.Nodes.List(),
	).Render(
		r.Context(),
		w,
	); err != nil {
		http.Error(
			w,
			err.Error(),
			http.StatusInternalServerError,
		)

		return
	}
}

func (h *Handler) NodesList(
	w http.ResponseWriter,
	r *http.Request,
) {
	if err := nodespage.NodesList(
		h.Nodes.List(),
	).Render(
		r.Context(),
		w,
	); err != nil {
		http.Error(
			w,
			"failed to render node list",
			http.StatusInternalServerError,
		)

		return
	}
}

func (h *Handler) SetNodeName(
	w http.ResponseWriter,
	r *http.Request,
) {
	nodeID := strings.TrimSpace(
		r.FormValue("node-id"),
	)

	name := strings.TrimSpace(
		r.FormValue("name"),
	)

	if nodeID == "" {
		http.Error(
			w,
			"node ID is required",
			http.StatusBadRequest,
		)

		return
	}

	if name == "" {
		http.Error(
			w,
			"node name is required",
			http.StatusBadRequest,
		)

		return
	}

	if _, ok := h.Nodes.Get(
		nodeID,
	); !ok {
		http.Error(
			w,
			fmt.Sprintf(
				"node %q not found",
				nodeID,
			),
			http.StatusNotFound,
		)

		return
	}

	if err := h.Nodes.SetName(
		nodeID,
		name,
	); err != nil {
		http.Error(
			w,
			fmt.Sprintf(
				"failed to update node %q: %v",
				nodeID,
				err,
			),
			http.StatusInternalServerError,
		)

		return
	}

	if err := nodespage.NodesList(
		h.Nodes.List(),
	).Render(
		r.Context(),
		w,
	); err != nil {
		http.Error(
			w,
			"failed to render node list",
			http.StatusInternalServerError,
		)

		return
	}
}
