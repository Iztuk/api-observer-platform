// Package nodes handles node operations.
package nodes

import (
	"api-observer/internal/config"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultHeartbeatTimeout = 45 * time.Second

type Node struct {
	ID   string
	Name string
	Addr string

	LastSeen time.Time
}

func (n *Node) Connected() bool {
	if n == nil {
		return false
	}

	if n.LastSeen.IsZero() {
		return false
	}

	return time.Since(
		n.LastSeen,
	) <= defaultHeartbeatTimeout
}

type NodeManager struct {
	mu sync.RWMutex

	nodes map[string]*Node

	appLogger *log.Logger

	config *config.Config
}

func NewNodeManager(
	appLogger *log.Logger,
	cfg *config.Config,
) *NodeManager {
	manager := &NodeManager{
		nodes: make(
			map[string]*Node,
		),

		appLogger: appLogger,
		config:    cfg,
	}

	manager.loadConfiguredNodes()

	return manager
}

func (m *NodeManager) Exists(
	nodeID string,
) bool {
	if nodeID == "" {
		return false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	_, ok :=
		m.nodes[nodeID]

	return ok
}

func (m *NodeManager) Add(
	nodeID string,
	addr string,
) error {
	nodeID =
		strings.TrimSpace(
			nodeID,
		)

	if nodeID == "" {
		return fmt.Errorf(
			"node ID is required",
		)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok :=
		m.nodes[nodeID]

	if ok {
		oldAddr :=
			existing.Addr

		existing.Addr =
			addr

		if m.config != nil {
			if err :=
				m.config.UpsertNode(
					nodeID,
					addr,
				); err != nil {

				existing.Addr =
					oldAddr

				return fmt.Errorf(
					"failed to update node configuration: %w",
					err,
				)
			}

			if err :=
				config.SaveConfigurationFile(
					m.config,
				); err != nil {

				existing.Addr =
					oldAddr

				_ = m.config.UpsertNode(
					nodeID,
					oldAddr,
				)

				return fmt.Errorf(
					"failed to persist node configuration: %w",
					err,
				)
			}
		}

		return nil
	}

	name :=
		nodeID

	if m.config != nil {
		if configuredName :=
			m.config.NodeName(
				nodeID,
			); configuredName != "" {

			name =
				configuredName
		}
	}

	node :=
		&Node{
			ID:   nodeID,
			Name: name,
			Addr: addr,
		}

	m.nodes[nodeID] =
		node

	if m.config != nil {
		if err :=
			m.config.UpsertNode(
				nodeID,
				addr,
			); err != nil {

			delete(
				m.nodes,
				nodeID,
			)

			return fmt.Errorf(
				"failed to add node configuration: %w",
				err,
			)
		}

		if err :=
			config.SaveConfigurationFile(
				m.config,
			); err != nil {

			delete(
				m.nodes,
				nodeID,
			)

			_ = m.config.RemoveNode(
				nodeID,
			)

			return fmt.Errorf(
				"failed to persist node configuration: %w",
				err,
			)
		}
	}

	if m.appLogger != nil {
		m.appLogger.Printf(
			"registered collector %q",
			nodeID,
		)
	}

	return nil
}

func (m *NodeManager) SetName(
	nodeID string,
	name string,
) error {
	nodeID =
		strings.TrimSpace(
			nodeID,
		)

	name =
		strings.TrimSpace(
			name,
		)

	if nodeID == "" {
		return fmt.Errorf(
			"node ID is required",
		)
	}

	if name == "" {
		return fmt.Errorf(
			"node name is required",
		)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	node, ok :=
		m.nodes[nodeID]

	if !ok {
		return fmt.Errorf(
			"node %q not found",
			nodeID,
		)
	}

	oldName :=
		node.Name

	node.Name =
		name

	if m.config != nil {
		if err :=
			m.config.SetNodeName(
				nodeID,
				name,
			); err != nil {

			node.Name =
				oldName

			return fmt.Errorf(
				"failed to update node name configuration: %w",
				err,
			)
		}

		if err :=
			config.SaveConfigurationFile(
				m.config,
			); err != nil {

			node.Name =
				oldName

			_ = m.config.SetNodeName(
				nodeID,
				oldName,
			)

			return fmt.Errorf(
				"failed to persist node name: %w",
				err,
			)
		}
	}

	if m.appLogger != nil {
		m.appLogger.Printf(
			"updated collector %q name to %q",
			nodeID,
			name,
		)
	}

	return nil
}

func (m *NodeManager) Heartbeat(
	nodeID string,
	receivedAt time.Time,
) error {
	nodeID =
		strings.TrimSpace(
			nodeID,
		)

	if nodeID == "" {
		return fmt.Errorf(
			"node ID is required",
		)
	}

	if receivedAt.IsZero() {
		receivedAt =
			time.Now()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	node, ok :=
		m.nodes[nodeID]

	if !ok {
		name :=
			nodeID

		if m.config != nil {
			name =
				m.config.NodeName(
					nodeID,
				)
		}

		node =
			&Node{
				ID:   nodeID,
				Name: name,
			}

		m.nodes[nodeID] =
			node
	}

	wasConnected :=
		node.Connected()

	node.LastSeen =
		receivedAt

	if !wasConnected &&
		m.appLogger != nil {

		m.appLogger.Printf(
			"collector %q connected",
			nodeID,
		)
	}

	return nil
}

func (m *NodeManager) Get(
	nodeID string,
) (*Node, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	node, ok :=
		m.nodes[nodeID]

	if !ok {
		return nil, false
	}

	return cloneNode(
		node,
	), true
}

func (m *NodeManager) List() []*Node {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(
		[]*Node,
		0,
		len(m.nodes),
	)

	for _, node := range m.nodes {

		result = append(
			result,
			cloneNode(
				node,
			),
		)
	}

	sort.Slice(
		result,
		func(
			i int,
			j int,
		) bool {
			return result[i].Name <
				result[j].Name
		},
	)

	return result
}

func (m *NodeManager) Remove(
	nodeID string,
) error {
	nodeID =
		strings.TrimSpace(
			nodeID,
		)

	if nodeID == "" {
		return fmt.Errorf(
			"node ID is required",
		)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	node, ok :=
		m.nodes[nodeID]

	if !ok {
		return fmt.Errorf(
			"node %q not found",
			nodeID,
		)
	}

	delete(
		m.nodes,
		nodeID,
	)

	if m.config != nil {
		if err :=
			m.config.RemoveNode(
				nodeID,
			); err != nil {

			m.nodes[nodeID] =
				node

			return fmt.Errorf(
				"failed to remove node configuration: %w",
				err,
			)
		}

		if err :=
			config.SaveConfigurationFile(
				m.config,
			); err != nil {

			m.nodes[nodeID] =
				node

			_ = m.config.UpsertNode(
				node.ID,
				node.Addr,
			)

			_ = m.config.SetNodeName(
				node.ID,
				node.Name,
			)

			return fmt.Errorf(
				"failed to persist node removal: %w",
				err,
			)
		}
	}

	if m.appLogger != nil {
		m.appLogger.Printf(
			"removed collector %q",
			nodeID,
		)
	}

	return nil
}

func (m *NodeManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	clear(
		m.nodes,
	)

	return nil
}

func (m *NodeManager) loadConfiguredNodes() {
	if m.config == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, configuredNode := range m.config.Nodes {

		if configuredNode.ID == "" {
			continue
		}

		name :=
			configuredNode.Name

		if name == "" {
			name =
				configuredNode.ID
		}

		m.nodes[configuredNode.ID] =
			&Node{
				ID:   configuredNode.ID,
				Name: name,
				Addr: configuredNode.Addr,
			}
	}
}

func cloneNode(
	node *Node,
) *Node {
	if node == nil {
		return nil
	}

	return &Node{
		ID:       node.ID,
		Name:     node.Name,
		Addr:     node.Addr,
		LastSeen: node.LastSeen,
	}
}
