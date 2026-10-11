// Package nodes handles node operations.
package nodes

import (
	"context"
	"fmt"
	"log"
	"sync"

	queryv1 "api-observer/proto/query/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Node struct {
	ID     string
	Name   string
	Addr   string
	Conn   *grpc.ClientConn
	Client queryv1.LogServiceClient
}

type NodeManager struct {
	mu sync.RWMutex

	nodes map[string]*Node

	ingestAddress string
	appLogger     *log.Logger
}

func NewNodeManager(
	ingestAddress string,
	appLogger *log.Logger,
) *NodeManager {
	return &NodeManager{
		nodes: make(
			map[string]*Node,
		),

		ingestAddress: ingestAddress,
		appLogger:     appLogger,
	}
}

func (m *NodeManager) Add(
	ctx context.Context,
	name string,
	addr string,
) error {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(
			insecure.NewCredentials(),
		),
	)
	if err != nil {
		m.appLogger.Printf(
			"failed to create client for node %q at %q: %v",
			name,
			addr,
			err,
		)

		return nil
	}

	client :=
		queryv1.NewLogServiceClient(
			conn,
		)

	info, err := client.Info(
		ctx,
		&queryv1.ObserverInfo{
			IngestAddress: m.ingestAddress,
		},
	)
	if err != nil {
		_ = conn.Close()

		m.appLogger.Printf(
			"failed to connect to node %q at %q: %v",
			name,
			addr,
			err,
		)

		return nil
	}

	if info.CollectorId == "" {
		_ = conn.Close()

		return fmt.Errorf(
			"collector at %q returned an empty collector ID",
			addr,
		)
	}

	node := &Node{
		ID:     info.CollectorId,
		Name:   name,
		Addr:   addr,
		Conn:   conn,
		Client: client,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok :=
		m.nodes[node.ID]; ok {

		if existing.Conn != nil {
			_ = existing.Conn.Close()
		}
	}

	m.nodes[node.ID] = node

	return nil
}

func (m *NodeManager) Get(
	nodeID string,
) (*Node, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	node, ok :=
		m.nodes[nodeID]

	return node, ok
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
			node,
		)
	}

	return nodes
}

func (m *NodeManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error

	for nodeID, node := range m.nodes {
		if node.Conn == nil {
			continue
		}

		if err :=
			node.Conn.Close(); err != nil &&
			firstErr == nil {

			firstErr = fmt.Errorf(
				"failed to close node %q: %w",
				nodeID,
				err,
			)
		}
	}

	return firstErr
}

func (m *NodeManager) Remove(
	nodeID string,
) error {
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

	if node.Conn != nil {
		if err :=
			node.Conn.Close(); err != nil {

			return fmt.Errorf(
				"failed to close node %q: %w",
				nodeID,
				err,
			)
		}
	}

	return nil
}
