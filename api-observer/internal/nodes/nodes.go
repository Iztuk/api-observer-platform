// Package nodes handles node operations.
package nodes

import (
	"fmt"
	"log"
	"sort"
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
}

func NewNodeManager(
	appLogger *log.Logger,
) *NodeManager {
	return &NodeManager{
		nodes: make(
			map[string]*Node,
		),

		appLogger: appLogger,
	}
}

func (m *NodeManager) Exists(nodeID string) bool {
	if nodeID == "" {
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.nodes[nodeID]

	return ok
}

func (m *NodeManager) Add(
	nodeID string,
	addr string,
) error {
	if nodeID == "" {
		return fmt.Errorf(
			"node ID is required",
		)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok := m.nodes[nodeID]
	if ok {
		existing.Addr = addr

		return nil
	}

	m.nodes[nodeID] = &Node{
		ID:   nodeID,
		Name: nodeID,
		Addr: addr,
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

	node, ok := m.nodes[nodeID]
	if !ok {
		return fmt.Errorf(
			"node %q not found",
			nodeID,
		)
	}

	node.Name = name

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
	if nodeID == "" {
		return fmt.Errorf(
			"node ID is required",
		)
	}

	if receivedAt.IsZero() {
		receivedAt = time.Now()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	node, ok := m.nodes[nodeID]
	if !ok {
		node = &Node{
			ID:   nodeID,
			Name: nodeID,
		}

		m.nodes[nodeID] = node
	}

	wasConnected := node.Connected()

	node.LastSeen = receivedAt

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

	node, ok := m.nodes[nodeID]
	if !ok {
		return nil, false
	}

	return cloneNode(node), true
}

func (m *NodeManager) List() []*Node {
	m.mu.RLock()
	defer m.mu.RUnlock()

	nodes := make(
		[]*Node,
		0,
		len(m.nodes),
	)

	for _, node := range m.nodes {
		nodes = append(
			nodes,
			cloneNode(node),
		)
	}

	sort.Slice(
		nodes,
		func(
			i int,
			j int,
		) bool {
			return nodes[i].Name <
				nodes[j].Name
		},
	)

	return nodes
}

func (m *NodeManager) Remove(
	nodeID string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.nodes[nodeID]; !ok {
		return fmt.Errorf(
			"node %q not found",
			nodeID,
		)
	}

	delete(
		m.nodes,
		nodeID,
	)

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
