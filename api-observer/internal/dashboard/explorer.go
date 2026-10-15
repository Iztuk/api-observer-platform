package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"api-observer/internal/audit"
	"api-observer/internal/dashboard/views/explorer"
	"api-observer/internal/nodes"
	"api-observer/internal/query"
	ingestv1 "api-observer/proto/ingest/v1"
	queryv1 "api-observer/proto/query/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	explorerDateTimeLayout = "2006-01-02T15:04"

	explorerEvaluationBatchSize = 100
	explorerSSEBatchSize        = 50

	collectorQueryPort = "24900"
)

type explorerParams struct {
	Query string

	Start time.Time
	End   time.Time

	StartString string
	EndString   string
}

type explorerSSELog struct {
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`

	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Method    string `json:"method,omitempty"`
	Path      string `json:"path,omitempty"`
	Status    int32  `json:"status,omitempty"`

	Findings []audit.Finding `json:"findings"`
}

type explorerSSEError struct {
	NodeID   string `json:"node_id,omitempty"`
	NodeName string `json:"node_name,omitempty"`
	Message  string `json:"message"`
}

type collectorLogBatch struct {
	NodeID   string
	NodeName string

	Logs query.LogItems

	Done bool
	Err  error
}

func (h *Handler) ExplorerPage(
	w http.ResponseWriter,
	r *http.Request,
) {
	params, err := parseExplorerParams(
		r,
	)
	if err != nil {
		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)

		return
	}

	if err := explorer.ExplorerPage(
		"Explorer",
		params.Query,
		params.StartString,
		params.EndString,
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

func (h *Handler) ExplorerLogs(
	w http.ResponseWriter,
	r *http.Request,
) {
	params, err := parseExplorerParams(
		r,
	)
	if err != nil {
		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)

		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(
			w,
			"streaming is not supported",
			http.StatusInternalServerError,
		)

		return
	}

	queryExpression, err := query.ParseQuery(
		params.Query,
	)
	if err != nil {
		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)

		return
	}

	targetNodes := make(
		[]*nodes.Node,
		0,
	)

	for _, node := range h.Nodes.List() {
		if !node.Connected() {
			continue
		}

		targetNodes = append(
			targetNodes,
			node,
		)
	}

	if len(targetNodes) == 0 {
		http.Error(
			w,
			"no connected nodes available",
			http.StatusServiceUnavailable,
		)

		return
	}

	w.Header().Set(
		"Content-Type",
		"text/event-stream",
	)

	w.Header().Set(
		"Cache-Control",
		"no-cache",
	)

	w.Header().Set(
		"Connection",
		"keep-alive",
	)

	w.Header().Set(
		"X-Accel-Buffering",
		"no",
	)

	flusher.Flush()

	ctx, cancel := context.WithCancel(
		r.Context(),
	)
	defer cancel()

	batches := make(
		chan collectorLogBatch,
		len(targetNodes)*2,
	)

	var wg sync.WaitGroup

	for _, node := range targetNodes {
		wg.Add(1)

		go func(
			node *nodes.Node,
		) {
			defer wg.Done()

			streamCollectorLogs(
				ctx,
				node,
				params,
				batches,
			)
		}(
			node,
		)
	}

	go func() {
		wg.Wait()

		close(
			batches,
		)
	}()

	totalMatches := 0
	completedNodes := 0
	failedNodes := 0

	sseBatch := make(
		[]explorerSSELog,
		0,
		explorerSSEBatchSize,
	)

	flushSSEBatch := func() error {
		if len(sseBatch) == 0 {
			return nil
		}

		if err := writeSSE(
			w,
			"logs",
			sseBatch,
		); err != nil {
			return err
		}

		flusher.Flush()

		sseBatch = sseBatch[:0]

		return nil
	}

	for batch := range batches {
		if batch.Err != nil {
			failedNodes++

			if err := writeSSE(
				w,
				"node_error",
				explorerSSEError{
					NodeID:   batch.NodeID,
					NodeName: batch.NodeName,
					Message:  batch.Err.Error(),
				},
			); err != nil {
				return
			}

			flusher.Flush()

			continue
		}

		if batch.Done {
			completedNodes++

			if err := writeSSE(
				w,
				"node_done",
				map[string]string{
					"node_id":   batch.NodeID,
					"node_name": batch.NodeName,
				},
			); err != nil {
				return
			}

			flusher.Flush()

			continue
		}

		if len(batch.Logs) == 0 {
			continue
		}

		if err := batch.Logs.GetFindings(
			ctx,
			h.DB,
			h.Config.FindingsLog,
			batch.NodeID,
			params.Start.UnixNano(),
			params.End.UnixNano(),
		); err != nil {
			if err := writeSSE(
				w,
				"node_error",
				explorerSSEError{
					NodeID:   batch.NodeID,
					NodeName: batch.NodeName,

					Message: fmt.Sprintf(
						"failed to retrieve findings: %v",
						err,
					),
				},
			); err != nil {
				return
			}

			flusher.Flush()

			continue
		}

		for _, item := range batch.Logs {
			matches, err := query.EvaluateExpression(
				params.Query,
				queryExpression,
				item,
			)
			if err != nil {
				if err := writeSSE(
					w,
					"node_error",
					explorerSSEError{
						NodeID:   batch.NodeID,
						NodeName: batch.NodeName,

						Message: fmt.Sprintf(
							"failed to evaluate query: %v",
							err,
						),
					},
				); err != nil {
					return
				}

				flusher.Flush()

				continue
			}

			if !matches {
				continue
			}

			result, err := logItemToSSE(
				item,
			)
			if err != nil {
				continue
			}

			result.NodeID =
				batch.NodeID

			result.NodeName =
				batch.NodeName

			sseBatch = append(
				sseBatch,
				result,
			)

			totalMatches++

			if len(sseBatch) >=
				explorerSSEBatchSize {

				if err := flushSSEBatch(); err != nil {
					return
				}
			}
		}
	}

	if err := flushSSEBatch(); err != nil {
		return
	}

	_ = writeSSE(
		w,
		"done",
		map[string]int{
			"matches":         totalMatches,
			"completed_nodes": completedNodes,
			"failed_nodes":    failedNodes,
		},
	)

	flusher.Flush()
}

func streamCollectorLogs(
	ctx context.Context,
	node *nodes.Node,
	params explorerParams,
	output chan<- collectorLogBatch,
) {
	queryAddress, err :=
		collectorQueryAddress(
			node,
		)

	if err != nil {
		sendCollectorBatch(
			ctx,
			output,
			collectorLogBatch{
				NodeID:   node.ID,
				NodeName: node.Name,
				Err:      err,
			},
		)

		return
	}

	conn, err := grpc.NewClient(
		queryAddress,
		grpc.WithTransportCredentials(
			insecure.NewCredentials(),
		),
	)
	if err != nil {
		sendCollectorBatch(
			ctx,
			output,
			collectorLogBatch{
				NodeID:   node.ID,
				NodeName: node.Name,

				Err: fmt.Errorf(
					"failed to connect to collector: %w",
					err,
				),
			},
		)

		return
	}
	defer conn.Close()

	client :=
		ingestv1.NewIngestServiceClient(
			conn,
		)

	stream, err := client.Logs(
		ctx,
		&ingestv1.LogRequest{
			StartDate: timestamppb.New(
				params.Start,
			),

			EndDate: timestamppb.New(
				params.End,
			),
		},
	)
	if err != nil {
		sendCollectorBatch(
			ctx,
			output,
			collectorLogBatch{
				NodeID:   node.ID,
				NodeName: node.Name,

				Err: fmt.Errorf(
					"failed to start log stream: %w",
					err,
				),
			},
		)

		return
	}

	logs := make(
		query.LogItems,
		0,
		explorerEvaluationBatchSize,
	)

	flush := func() bool {
		if len(logs) == 0 {
			return true
		}

		batch := make(
			query.LogItems,
			len(logs),
		)

		copy(
			batch,
			logs,
		)

		if !sendCollectorBatch(
			ctx,
			output,
			collectorLogBatch{
				NodeID:   node.ID,
				NodeName: node.Name,
				Logs:     batch,
			},
		) {
			return false
		}

		logs = logs[:0]

		return true
	}

	for {
		transaction, err :=
			stream.Recv()

		if errors.Is(
			err,
			io.EOF,
		) {
			if !flush() {
				return
			}

			sendCollectorBatch(
				ctx,
				output,
				collectorLogBatch{
					NodeID:   node.ID,
					NodeName: node.Name,
					Done:     true,
				},
			)

			return
		}

		if err != nil {
			if ctx.Err() != nil {
				return
			}

			sendCollectorBatch(
				ctx,
				output,
				collectorLogBatch{
					NodeID:   node.ID,
					NodeName: node.Name,

					Err: fmt.Errorf(
						"log stream failed: %w",
						err,
					),
				},
			)

			return
		}

		if transaction == nil {
			continue
		}

		if transaction.Request != nil {
			logs = append(
				logs,
				query.LogItem{
					Log: requestToQueryLog(
						transaction.Request,
					),
					NodeName: node.Name,
				},
			)
		}

		if transaction.Response != nil {
			logs = append(
				logs,
				query.LogItem{
					Log: responseToQueryLog(
						transaction.Response,
					),
					NodeName: node.Name,
				},
			)
		}

		if len(logs) >=
			explorerEvaluationBatchSize {

			if !flush() {
				return
			}
		}
	}
}

func sendCollectorBatch(
	ctx context.Context,
	output chan<- collectorLogBatch,
	batch collectorLogBatch,
) bool {
	select {
	case output <- batch:
		return true

	case <-ctx.Done():
		return false
	}
}

func collectorQueryAddress(
	node *nodes.Node,
) (string, error) {
	if node == nil {
		return "",
			fmt.Errorf(
				"node is nil",
			)
	}

	if node.Addr == "" {
		return "",
			fmt.Errorf(
				"node %q does not have an address",
				node.ID,
			)
	}

	host, _, err := net.SplitHostPort(
		node.Addr,
	)
	if err != nil {
		return "",
			fmt.Errorf(
				"invalid node address %q: %w",
				node.Addr,
				err,
			)
	}

	return net.JoinHostPort(
		host,
		collectorQueryPort,
	), nil
}

func writeSSE(
	w io.Writer,
	event string,
	value any,
) error {
	data, err := json.Marshal(
		value,
	)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(
		w,
		"event: %s\ndata: %s\n\n",
		event,
		data,
	); err != nil {
		return err
	}

	return nil
}

func logItemToSSE(
	item query.LogItem,
) (explorerSSELog, error) {
	if item.Log == nil {
		return explorerSSELog{},
			fmt.Errorf(
				"log item is nil",
			)
	}

	switch event :=
		item.Log.Event.(type) {

	case *queryv1.Log_Request:
		if event.Request == nil {
			return explorerSSELog{},
				fmt.Errorf(
					"request is nil",
				)
		}

		result := explorerSSELog{
			Type:   "Request",
			Method: event.Request.Method,
			Path:   event.Request.Url,

			Findings: append(
				[]audit.Finding(nil),
				item.Findings...,
			),
		}

		if event.Request.Metadata != nil &&
			event.Request.Metadata.Timestamp != nil {

			result.Timestamp =
				event.Request.Metadata.
					Timestamp.
					AsTime().
					Format(
						time.RFC3339Nano,
					)
		}

		return result, nil

	case *queryv1.Log_Response:
		if event.Response == nil {
			return explorerSSELog{},
				fmt.Errorf(
					"response is nil",
				)
		}

		result := explorerSSELog{
			Type:   "Response",
			Status: event.Response.StatusCode,

			Findings: append(
				[]audit.Finding(nil),
				item.Findings...,
			),
		}

		if event.Response.Metadata != nil &&
			event.Response.Metadata.Timestamp != nil {

			result.Timestamp =
				event.Response.Metadata.
					Timestamp.
					AsTime().
					Format(
						time.RFC3339Nano,
					)
		}

		return result, nil
	default:
		return explorerSSELog{},
			fmt.Errorf(
				"unsupported log event type %T",
				item.Log.Event,
			)
	}
}

func requestToQueryLog(
	request *ingestv1.Request,
) *queryv1.Log {
	if request == nil {
		return nil
	}

	result := &queryv1.Request{
		Method: request.Method,
		Url:    request.Url,

		Headers: convertIngestHeaders(
			request.Headers,
		),

		Body:          request.Body,
		ContentLength: request.ContentLength,
	}

	if request.Metadata != nil {
		result.Metadata = &queryv1.Metadata{
			RequestId: request.Metadata.RequestId,

			Source: request.Metadata.Source,

			Timestamp: request.Metadata.Timestamp,
		}
	}

	return &queryv1.Log{
		Event: &queryv1.Log_Request{
			Request: result,
		},
	}
}

func responseToQueryLog(
	response *ingestv1.Response,
) *queryv1.Log {
	if response == nil {
		return nil
	}

	result := &queryv1.Response{
		StatusCode: response.StatusCode,

		Headers: convertIngestHeaders(
			response.Headers,
		),

		Body: response.Body,

		ContentLength: response.ContentLength,
	}

	if response.Metadata != nil {
		result.Metadata = &queryv1.Metadata{
			RequestId: response.Metadata.RequestId,

			Source: response.Metadata.Source,

			Timestamp: response.Metadata.Timestamp,
		}
	}

	return &queryv1.Log{
		Event: &queryv1.Log_Response{
			Response: result,
		},
	}
}

func convertIngestHeaders(
	headers map[string]*ingestv1.HeaderValues,
) map[string]*queryv1.HeaderValues {
	result := make(
		map[string]*queryv1.HeaderValues,
		len(headers),
	)

	for name, value := range headers {
		if value == nil {
			continue
		}

		result[name] =
			&queryv1.HeaderValues{
				Values: append(
					[]string(nil),
					value.Values...,
				),
			}
	}

	return result
}

func parseExplorerParams(
	r *http.Request,
) (explorerParams, error) {
	queries := r.URL.Query()

	params := explorerParams{
		Query: queries.Get(
			"query",
		),

		StartString: queries.Get(
			"start",
		),

		EndString: queries.Get(
			"end",
		),
	}

	now := time.Now()

	if params.EndString == "" {
		params.End = now

		params.EndString =
			params.End.Format(
				explorerDateTimeLayout,
			)
	} else {
		end, err := parseExplorerTime(
			params.EndString,
		)
		if err != nil {
			return explorerParams{},
				fmt.Errorf(
					"invalid end time: %w",
					err,
				)
		}

		params.End = end
	}

	if params.StartString == "" {
		params.Start =
			params.End.Add(
				-24 * time.Hour,
			)

		params.StartString =
			params.Start.Format(
				explorerDateTimeLayout,
			)
	} else {
		start, err := parseExplorerTime(
			params.StartString,
		)
		if err != nil {
			return explorerParams{},
				fmt.Errorf(
					"invalid start time: %w",
					err,
				)
		}

		params.Start = start
	}

	if params.End.Before(
		params.Start,
	) {
		return explorerParams{},
			fmt.Errorf(
				"end time must be after start time",
			)
	}

	return params, nil
}

func parseExplorerTime(
	value string,
) (time.Time, error) {
	if parsed, err := time.Parse(
		time.RFC3339,
		value,
	); err == nil {
		return parsed, nil
	}

	parsed, err := time.ParseInLocation(
		explorerDateTimeLayout,
		value,
		time.Local,
	)
	if err != nil {
		return time.Time{},
			fmt.Errorf(
				"invalid datetime %q: %w",
				value,
				err,
			)
	}

	return parsed, nil
}

func logToJob(
	logRecord *queryv1.Log,
) (audit.Job, error) {
	if logRecord == nil {
		return audit.Job{},
			fmt.Errorf(
				"log record is nil",
			)
	}

	switch event := logRecord.Event.(type) {
	case *queryv1.Log_Request:
		if event.Request == nil {
			return audit.Job{},
				fmt.Errorf(
					"request is nil",
				)
		}

		request := event.Request

		requestURL, err := url.Parse(
			request.Url,
		)
		if err != nil {
			return audit.Job{},
				fmt.Errorf(
					"failed to parse request URL: %w",
					err,
				)
		}

		job := audit.Job{
			Request: &audit.RequestJob{
				Method: request.Method,
				URL:    requestURL,

				Headers: toHTTPHeader(
					request.Headers,
				),

				Body: request.Body,

				ContentLength: request.ContentLength,
			},
		}

		if request.Metadata != nil {
			job.Request.Metadata =
				audit.Metadata{
					RequestID: request.Metadata.RequestId,

					Source: request.Metadata.Source,
				}

			if request.Metadata.Timestamp != nil {
				job.Request.Metadata.Timestamp =
					request.Metadata.
						Timestamp.
						AsTime().
						UnixNano()
			}
		}

		return job, nil

	case *queryv1.Log_Response:
		if event.Response == nil {
			return audit.Job{},
				fmt.Errorf(
					"response is nil",
				)
		}

		response := event.Response

		job := audit.Job{
			Response: &audit.ResponseJob{
				StatusCode: int(
					response.StatusCode,
				),

				Headers: toHTTPHeader(
					response.Headers,
				),

				Body: response.Body,

				ContentLength: response.ContentLength,
			},
		}

		if response.Metadata != nil {
			job.Response.Metadata =
				audit.Metadata{
					RequestID: response.Metadata.RequestId,

					Source: response.Metadata.Source,
				}

			if response.Metadata.Timestamp != nil {
				job.Response.Metadata.Timestamp =
					response.Metadata.
						Timestamp.
						AsTime().
						UnixNano()
			}
		}

		return job, nil
	}

	return audit.Job{},
		fmt.Errorf(
			"unsupported log event type %T",
			logRecord.Event,
		)
}

func toHTTPHeader(
	headers map[string]*queryv1.HeaderValues,
) http.Header {
	result := http.Header{}

	for key, value := range headers {
		if value == nil {
			continue
		}

		result[key] = append(
			[]string(nil),
			value.Values...,
		)
	}

	return result
}
