package log

import (
	"container/heap"
	"context"
	"fmt"
	"sync"
	"time"

	"api-observer/internal/nodes"
	"api-observer/internal/query"
	ingestv1 "api-observer/proto/ingest/v1"
)

type StreamEvent struct {
	NodeID   string
	NodeName string
	Log      *StreamLog
	Done     bool
	Err      error
}

type StreamLog struct {
	NodeID   string
	NodeName string
	Item     query.LogItem
}

type nodeQueue struct {
	NodeID       string
	NodeName     string
	Batches      []query.LogItems
	BatchIndex   int
	ItemIndex    int
	Done         bool
	Err          error
	terminalSent bool
}

type mergeItem struct {
	NodeID    string
	NodeName  string
	Item      query.LogItem
	Timestamp time.Time
	RequestID string
	EventType int
}

type logHeap []*mergeItem

func StreamMergedLogs(ctx context.Context, targetNodes []*nodes.Node, params StreamParams) <-chan StreamEvent {
	output := make(chan StreamEvent)

	go func() {
		defer close(output)
		streamMergedLogs(ctx, targetNodes, params, output)
	}()

	return output
}

func streamMergedLogs(ctx context.Context, targetNodes []*nodes.Node, params StreamParams, output chan<- StreamEvent) {
	if len(targetNodes) == 0 {
		return
	}

	batches := make(chan StreamLogBatch, len(targetNodes)*2)

	var wg sync.WaitGroup
	activeNodes := 0

	for _, node := range targetNodes {
		if node == nil {
			continue
		}

		activeNodes++
		wg.Add(1)

		go func(node *nodes.Node) {
			defer wg.Done()
			StreamCollectorLogs(ctx, node, params, batches)
		}(node)
	}

	if activeNodes == 0 {
		return
	}

	go func() {
		wg.Wait()
		close(batches)
	}()

	mergeStreams(ctx, batches, output, activeNodes)
}

func mergeStreams(ctx context.Context, input <-chan StreamLogBatch, output chan<- StreamEvent, nodeCount int) {
	queues := make(map[string]*nodeQueue, nodeCount)
	inHeap := make(map[string]bool, nodeCount)

	candidates := &logHeap{}
	heap.Init(candidates)

	inputClosed := false

	for {
		for _, queue := range queues {
			if inHeap[queue.NodeID] {
				continue
			}

			if queue.Err != nil && queue.empty() {
				continue
			}

			item, ok := queue.next()
			if !ok {
				continue
			}

			candidate, err := newMergeItem(queue.NodeID, queue.NodeName, item)
			if err != nil {
				queue.Err = fmt.Errorf("failed to build log sort key: %w", err)
				queue.clearBuffered()
				continue
			}

			heap.Push(candidates, candidate)
			inHeap[queue.NodeID] = true
		}

		for _, queue := range queues {
			if queue.terminalSent {
				continue
			}

			if inHeap[queue.NodeID] {
				continue
			}

			if !queue.empty() {
				continue
			}

			if queue.Err != nil {
				if !sendStreamEvent(ctx, output, StreamEvent{
					NodeID:   queue.NodeID,
					NodeName: queue.NodeName,
					Err:      queue.Err,
				}) {
					return
				}

				queue.terminalSent = true
				continue
			}

			if queue.Done {
				if !sendStreamEvent(ctx, output, StreamEvent{
					NodeID:   queue.NodeID,
					NodeName: queue.NodeName,
					Done:     true,
				}) {
					return
				}

				queue.terminalSent = true
			}
		}

		if candidates.Len() > 0 && canEmit(queues, inHeap, nodeCount) {
			candidate := heap.Pop(candidates).(*mergeItem)
			inHeap[candidate.NodeID] = false

			if !sendStreamEvent(ctx, output, StreamEvent{
				NodeID:   candidate.NodeID,
				NodeName: candidate.NodeName,
				Log: &StreamLog{
					NodeID:   candidate.NodeID,
					NodeName: candidate.NodeName,
					Item:     candidate.Item,
				},
			}) {
				return
			}

			continue
		}

		if inputClosed && candidates.Len() == 0 && allQueuesDrained(queues) {
			return
		}

		if inputClosed {
			for _, queue := range queues {
				if queue.Done || queue.Err != nil {
					continue
				}

				queue.Err = fmt.Errorf("collector stream closed unexpectedly")
			}

			continue
		}

		select {
		case <-ctx.Done():
			return

		case batch, ok := <-input:
			if !ok {
				inputClosed = true
				continue
			}

			queue := queues[batch.NodeID]
			if queue == nil {
				queue = &nodeQueue{
					NodeID:   batch.NodeID,
					NodeName: batch.NodeName,
				}

				queues[batch.NodeID] = queue
			}

			if queue.NodeName == "" && batch.NodeName != "" {
				queue.NodeName = batch.NodeName
			}

			switch {
			case batch.Err != nil:
				queue.Err = batch.Err

			case batch.Done:
				queue.Done = true

			case len(batch.Logs) > 0:
				queue.appendBatch(batch.Logs)
			}
		}
	}
}

func (q *nodeQueue) appendBatch(logs query.LogItems) {
	if len(logs) == 0 {
		return
	}

	if q.BatchIndex >= len(q.Batches) {
		q.Batches = q.Batches[:0]
		q.BatchIndex = 0
		q.ItemIndex = 0
	}

	q.Batches = append(q.Batches, logs)
}

func (q *nodeQueue) next() (query.LogItem, bool) {
	if q.empty() {
		return query.LogItem{}, false
	}

	batch := q.Batches[q.BatchIndex]
	item := batch[q.ItemIndex]

	q.ItemIndex++

	if q.ItemIndex >= len(batch) {
		q.Batches[q.BatchIndex] = nil
		q.BatchIndex++
		q.ItemIndex = 0

		if q.BatchIndex >= 64 && q.BatchIndex < len(q.Batches) {
			remaining := copy(q.Batches, q.Batches[q.BatchIndex:])
			q.Batches = q.Batches[:remaining]
			q.BatchIndex = 0
		}

		if q.BatchIndex >= len(q.Batches) {
			q.Batches = q.Batches[:0]
			q.BatchIndex = 0
		}
	}

	return item, true
}

func (q *nodeQueue) empty() bool {
	return q.BatchIndex >= len(q.Batches)
}

func (q *nodeQueue) clearBuffered() {
	for i := q.BatchIndex; i < len(q.Batches); i++ {
		q.Batches[i] = nil
	}

	q.Batches = q.Batches[:0]
	q.BatchIndex = 0
	q.ItemIndex = 0
}

func canEmit(queues map[string]*nodeQueue, inHeap map[string]bool, nodeCount int) bool {
	if len(queues) < nodeCount {
		return false
	}

	for _, queue := range queues {
		if inHeap[queue.NodeID] {
			continue
		}

		if !queue.empty() {
			return false
		}

		if queue.Done || queue.Err != nil {
			continue
		}

		return false
	}

	return true
}

func allQueuesDrained(queues map[string]*nodeQueue) bool {
	for _, queue := range queues {
		if !queue.empty() {
			return false
		}

		if !queue.Done && queue.Err == nil {
			return false
		}
	}

	return true
}

func sendStreamEvent(ctx context.Context, output chan<- StreamEvent, event StreamEvent) bool {
	select {
	case output <- event:
		return true

	case <-ctx.Done():
		return false
	}
}

func newMergeItem(nodeID string, nodeName string, item query.LogItem) (*mergeItem, error) {
	timestamp, requestID, eventType, err := logSortKey(item)
	if err != nil {
		return nil, err
	}

	return &mergeItem{
		NodeID:    nodeID,
		NodeName:  nodeName,
		Item:      item,
		Timestamp: timestamp,
		RequestID: requestID,
		EventType: eventType,
	}, nil
}

func logSortKey(item query.LogItem) (time.Time, string, int, error) {
	if item.Log == nil {
		return time.Time{}, "", 0, fmt.Errorf("log item is nil")
	}

	switch event := item.Log.Event.(type) {
	case *ingestv1.Record_Request:
		if event.Request == nil {
			return time.Time{}, "", 0, fmt.Errorf("request is nil")
		}

		if event.Request.Metadata == nil {
			return time.Time{}, "", 0, fmt.Errorf("request metadata is nil")
		}

		if event.Request.Metadata.Timestamp == nil {
			return time.Time{}, "", 0, fmt.Errorf("request timestamp is nil")
		}

		return event.Request.Metadata.Timestamp.AsTime(),
			event.Request.Metadata.RequestId,
			0,
			nil

	case *ingestv1.Record_Response:
		if event.Response == nil {
			return time.Time{}, "", 0, fmt.Errorf("response is nil")
		}

		if event.Response.Metadata == nil {
			return time.Time{}, "", 0, fmt.Errorf("response metadata is nil")
		}

		if event.Response.Metadata.Timestamp == nil {
			return time.Time{}, "", 0, fmt.Errorf("response timestamp is nil")
		}

		return event.Response.Metadata.Timestamp.AsTime(),
			event.Response.Metadata.RequestId,
			1,
			nil

	default:
		return time.Time{}, "", 0, fmt.Errorf("unsupported log event type %T", item.Log.Event)
	}
}

func (h logHeap) Len() int {
	return len(h)
}

func (h logHeap) Less(i, j int) bool {
	left := h[i]
	right := h[j]

	if !left.Timestamp.Equal(right.Timestamp) {
		return left.Timestamp.After(right.Timestamp)
	}

	if left.NodeID != right.NodeID {
		return left.NodeID < right.NodeID
	}

	if left.RequestID != right.RequestID {
		return left.RequestID < right.RequestID
	}

	return left.EventType < right.EventType
}

func (h logHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *logHeap) Push(value any) {
	*h = append(*h, value.(*mergeItem))
}

func (h *logHeap) Pop() any {
	old := *h
	n := len(old)

	item := old[n-1]
	old[n-1] = nil

	*h = old[:n-1]

	return item
}
