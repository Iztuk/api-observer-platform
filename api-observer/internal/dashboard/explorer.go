package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"api-observer/internal/audit"
	"api-observer/internal/dashboard/views/explorer"
	"api-observer/internal/log"
	"api-observer/internal/nodes"
	"api-observer/internal/query"
	ingestv1 "api-observer/proto/ingest/v1"
)

const (
	explorerDateTimeLayout      = "2006-01-02T15:04"
	explorerEnrichmentBatchSize = 1000
)

type explorerSSELog struct {
	NodeID    string `json:"node_id"`
	NodeName  string `json:"node_name"`
	RequestID string `json:"request_id"`

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

func (h *Handler) ExplorerPage(w http.ResponseWriter, r *http.Request) {
	params, err := parseExplorerParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	connectedNodes := getConnectedExplorerNodes(h.Nodes.List())

	if err := explorer.ExplorerPage(
		"Explorer",
		params.Query,
		params.StartString,
		params.EndString,
		len(connectedNodes) > 0,
	).Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *Handler) ExplorerLogs(w http.ResponseWriter, r *http.Request) {
	params, err := parseExplorerParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}

	queryExpression, err := query.ParseQuery(params.Query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	targetNodes := getConnectedExplorerNodes(h.Nodes.List())
	if len(targetNodes) == 0 {
		http.Error(w, "no connected nodes available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher.Flush()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	stream := log.StreamMergedLogs(ctx, targetNodes, params)

	type mergedItem struct {
		NodeID   string
		NodeName string
		Item     query.LogItem
	}

	batch := make([]mergedItem, 0, explorerEnrichmentBatchSize)

	totalMatches := 0
	completedNodes := 0
	failedNodes := 0

	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}

		items := make(query.LogItems, len(batch))

		for i := range batch {
			items[i] = batch[i].Item
		}

		if err := items.GetFindings(
			ctx,
			h.DB,
			h.Config.FindingsLog,
		); err != nil {
			return fmt.Errorf("failed to retrieve findings: %w", err)
		}

		results := make([]explorerSSELog, 0, len(items))

		for i := range items {
			item := items[i]

			matches, err := query.EvaluateExpression(
				params.Query,
				queryExpression,
				item,
			)
			if err != nil {
				return fmt.Errorf("failed to evaluate query: %w", err)
			}

			if !matches {
				continue
			}

			result, err := logItemToSSE(item)
			if err != nil {
				continue
			}

			result.NodeID = batch[i].NodeID
			result.NodeName = batch[i].NodeName

			results = append(results, result)
		}

		batch = batch[:0]

		if len(results) == 0 {
			return nil
		}

		totalMatches += len(results)

		if err := writeSSE(w, "logs", results); err != nil {
			return err
		}

		flusher.Flush()

		return nil
	}

	for event := range stream {
		if event.Err != nil {
			failedNodes++

			if err := writeSSE(w, "node_error", explorerSSEError{
				NodeID:   event.NodeID,
				NodeName: event.NodeName,
				Message:  event.Err.Error(),
			}); err != nil {
				return
			}

			flusher.Flush()
			continue
		}

		if event.Done {
			completedNodes++

			if err := writeSSE(w, "node_done", map[string]string{
				"node_id":   event.NodeID,
				"node_name": event.NodeName,
			}); err != nil {
				return
			}

			flusher.Flush()
			continue
		}

		if event.Log == nil {
			continue
		}

		batch = append(batch, mergedItem{
			NodeID:   event.NodeID,
			NodeName: event.NodeName,
			Item:     event.Log.Item,
		})

		if len(batch) < explorerEnrichmentBatchSize {
			continue
		}

		if err := flushBatch(); err != nil {
			failedNodes++

			if err := writeSSE(w, "node_error", explorerSSEError{
				Message: err.Error(),
			}); err != nil {
				return
			}

			flusher.Flush()
			break
		}
	}

	if len(batch) > 0 {
		if err := flushBatch(); err != nil {
			failedNodes++

			if err := writeSSE(w, "node_error", explorerSSEError{
				Message: err.Error(),
			}); err != nil {
				return
			}

			flusher.Flush()
		}
	}

	if err := writeSSE(w, "done", map[string]int{
		"matches":         totalMatches,
		"completed_nodes": completedNodes,
		"failed_nodes":    failedNodes,
	}); err != nil {
		return
	}

	flusher.Flush()
}

func getConnectedExplorerNodes(
	allNodes []*nodes.Node,
) []*nodes.Node {
	result := make(
		[]*nodes.Node,
		0,
		len(allNodes),
	)

	for _, node := range allNodes {

		if node == nil {
			continue
		}

		if !node.Connected() {
			continue
		}

		result =
			append(
				result,
				node,
			)
	}

	return result
}

func findExplorerNode(
	allNodes []*nodes.Node,
	nodeID string,
) *nodes.Node {
	for _, node := range allNodes {

		if node == nil {
			continue
		}

		if node.ID ==
			nodeID {

			return node
		}
	}

	return nil
}

func writeSSE(
	w io.Writer,
	event string,
	value any,
) error {
	data, err :=
		json.Marshal(
			value,
		)

	if err != nil {
		return err
	}

	if _, err :=
		fmt.Fprintf(
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

	case *ingestv1.Record_Request:
		if event.Request == nil {
			return explorerSSELog{},
				fmt.Errorf(
					"request is nil",
				)
		}

		result :=
			explorerSSELog{
				NodeName: item.NodeName,

				Type: "Request",

				Method: event.Request.Method,

				Path: event.Request.Url,

				Findings: append(
					[]audit.Finding(nil),
					item.Findings...,
				),
			}

		if event.Request.Metadata != nil {
			result.RequestID =
				event.Request.Metadata.RequestId

			if event.Request.Metadata.Timestamp != nil {
				result.Timestamp =
					event.Request.Metadata.
						Timestamp.
						AsTime().
						Format(
							time.RFC3339Nano,
						)
			}
		}

		return result,
			nil

	case *ingestv1.Record_Response:
		if event.Response == nil {
			return explorerSSELog{},
				fmt.Errorf(
					"response is nil",
				)
		}

		result :=
			explorerSSELog{
				NodeName: item.NodeName,

				Type: "Response",

				Status: event.Response.StatusCode,

				Findings: append(
					[]audit.Finding(nil),
					item.Findings...,
				),
			}

		if event.Response.Metadata != nil {
			result.RequestID =
				event.Response.Metadata.RequestId

			if event.Response.Metadata.Timestamp != nil {
				result.Timestamp =
					event.Response.Metadata.
						Timestamp.
						AsTime().
						Format(
							time.RFC3339Nano,
						)
			}
		}

		return result,
			nil

	default:
		return explorerSSELog{},
			fmt.Errorf(
				"unsupported log event type %T",
				item.Log.Event,
			)
	}
}

func parseExplorerParams(
	r *http.Request,
) (log.StreamParams, error) {
	queries :=
		r.URL.Query()

	params :=
		log.StreamParams{
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

	now :=
		time.Now()

	if params.EndString == "" {
		params.End =
			now

		params.EndString =
			params.End.Format(
				explorerDateTimeLayout,
			)
	} else {
		end, err :=
			parseExplorerTime(
				params.EndString,
			)

		if err != nil {
			return log.StreamParams{},
				fmt.Errorf(
					"invalid end time: %w",
					err,
				)
		}

		params.End =
			end
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
		start, err :=
			parseExplorerTime(
				params.StartString,
			)

		if err != nil {
			return log.StreamParams{},
				fmt.Errorf(
					"invalid start time: %w",
					err,
				)
		}

		params.Start =
			start
	}

	if params.End.Before(
		params.Start,
	) {
		return log.StreamParams{},
			fmt.Errorf(
				"end time must be after start time",
			)
	}

	return params,
		nil
}

func parseExplorerTime(
	value string,
) (time.Time, error) {
	if parsed, err :=
		time.Parse(
			time.RFC3339,
			value,
		); err == nil {

		return parsed,
			nil
	}

	parsed, err :=
		time.ParseInLocation(
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

	return parsed,
		nil
}

func logToJob(
	logRecord *ingestv1.Record,
) (audit.Job, error) {
	if logRecord == nil {
		return audit.Job{},
			fmt.Errorf(
				"log record is nil",
			)
	}

	switch event :=
		logRecord.Event.(type) {

	case *ingestv1.Record_Request:
		if event.Request == nil {
			return audit.Job{},
				fmt.Errorf(
					"request is nil",
				)
		}

		request :=
			event.Request

		requestURL, err :=
			url.Parse(
				request.Url,
			)

		if err != nil {
			return audit.Job{},
				fmt.Errorf(
					"failed to parse request URL: %w",
					err,
				)
		}

		job :=
			audit.Job{
				Type: audit.JobTypeRequest,

				Request: &audit.RequestJob{
					Method: request.Method,

					URL: requestURL,

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

		return job,
			nil

	case *ingestv1.Record_Response:
		if event.Response == nil {
			return audit.Job{},
				fmt.Errorf(
					"response is nil",
				)
		}

		response :=
			event.Response

		job :=
			audit.Job{
				Type: audit.JobTypeResponse,

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

		return job,
			nil

	default:
		return audit.Job{},
			fmt.Errorf(
				"unsupported log event type %T",
				logRecord.Event,
			)
	}
}

func toHTTPHeader(
	headers map[string]*ingestv1.HeaderValues,
) http.Header {
	result :=
		http.Header{}

	for key, value := range headers {

		if value == nil {
			continue
		}

		result[key] =
			append(
				[]string(nil),
				value.Values...,
			)
	}

	return result
}
