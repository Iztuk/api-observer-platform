package dashboard

import (
	"api-observer/internal/config"
	nodespage "api-observer/internal/dashboard/views/nodes-page"
	"api-observer/internal/nodes"
	"fmt"
	"net/http"
	"slices"
)

func (h *Handler) NodesPage(w http.ResponseWriter, r *http.Request) {
	if err := nodespage.NodesPage("Nodes", h.Nodes.List()).Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *Handler) NodesList(w http.ResponseWriter, r *http.Request) {
	if err := nodespage.NodesList(
		h.Nodes.List(),
	).Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render node list", http.StatusInternalServerError)
		return
	}
}

func (h *Handler) AddNode(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	addr := r.FormValue("addr")

	found := slices.ContainsFunc(
		h.Nodes.List(),
		func(node *nodes.Node) bool {
			return node.Name == name
		},
	)

	if found {
		http.Error(w, fmt.Sprintf("node '%s' already exists", name), http.StatusConflict)
		return
	}

	if err := h.Nodes.Add(r.Context(), name, addr); err != nil {
		http.Error(w, "failed to add node", http.StatusInternalServerError)
		return
	}

	h.Config.Nodes = convertNodesToConfigNodes(h.Nodes.List())
	if err := config.SaveConfigurationFile(h.Config); err != nil {
		http.Error(w, "failed to save changes", http.StatusInternalServerError)
		h.Nodes.Remove(name)
		return
	}

	if err := nodespage.NodesList(
		h.Nodes.List(),
	).Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render node list", http.StatusInternalServerError)
		return
	}
}

func (h *Handler) DeleteNode(w http.ResponseWriter, r *http.Request) {
	nodeId := r.FormValue("delete-node-id")

	found := slices.ContainsFunc(
		h.Nodes.List(),
		func(node *nodes.Node) bool {
			return node.ID == nodeId
		},
	)

	if !found {
		http.Error(w, fmt.Sprintf("node '%s' not found", nodeId), http.StatusNotFound)
		return
	}

	if err := h.Nodes.Remove(nodeId); err != nil {
		http.Error(w, fmt.Sprintf("failed to remove node '%s'", nodeId), http.StatusInternalServerError)
		return
	}

	h.Config.Nodes = convertNodesToConfigNodes(h.Nodes.List())
	if err := config.SaveConfigurationFile(h.Config); err != nil {
		http.Error(w, "failed to save changes", http.StatusInternalServerError)
		h.Nodes.Remove(nodeId)
		return
	}

	if err := nodespage.NodesList(
		h.Nodes.List(),
	).Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render node list", http.StatusInternalServerError)
		return
	}
}

func convertNodesToConfigNodes(nodes []*nodes.Node) []config.NodeConfig {
	result := make([]config.NodeConfig, 0)

	for _, node := range nodes {
		cn := config.NodeConfig{
			Name: node.Name,
			Addr: node.Addr,
		}
		result = append(result, cn)
	}

	return result
}
