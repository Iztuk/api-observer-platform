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
	explorerDateTimeLayout = "2006-01-02T15:04"
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

func (h *Handler) ExplorerPage(
	w http.ResponseWriter,
	r *http.Request,
) {
	params, err :=
		parseExplorerParams(
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

	connectedNodes :=
		getConnectedExplorerNodes(
			h.Nodes.List(),
		)

	nodeOptions := make(
		[]explorer.NodeOption,
		0,
		len(connectedNodes),
	)

	for _, node := range connectedNodes {

		nodeOptions =
			append(
				nodeOptions,
				explorer.NodeOption{
					ID: node.ID,

					Name: node.Name,
				},
			)
	}

	selectedNodeID :=
		r.URL.Query().Get(
			"node",
		)

	/*
	 * If the page is opened without an explicit node,
	 * select the first connected node.
	 *
	 * The form will then include this node when the
	 * frontend opens /logs.
	 */
	if selectedNodeID == "" &&
		len(connectedNodes) > 0 {

		selectedNodeID =
			connectedNodes[0].ID
	}

	/*
	 * If the URL contains a stale/disconnected node,
	 * fall back to the first currently connected one.
	 */
	if selectedNodeID != "" &&
		findExplorerNode(
			connectedNodes,
			selectedNodeID,
		) == nil {

		if len(connectedNodes) > 0 {
			selectedNodeID =
				connectedNodes[0].ID
		} else {
			selectedNodeID =
				""
		}
	}

	if err :=
		explorer.ExplorerPage(
			"Explorer",
			params.Query,
			params.StartString,
			params.EndString,
			nodeOptions,
			selectedNodeID,
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
	params, err :=
		parseExplorerParams(
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

	flusher, ok :=
		w.(http.Flusher)

	if !ok {
		http.Error(
			w,
			"streaming is not supported",
			http.StatusInternalServerError,
		)

		return
	}

	queryExpression, err :=
		query.ParseQuery(
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

	nodeID :=
		r.URL.Query().Get(
			"node",
		)

	if nodeID == "" {
		http.Error(
			w,
			"node is required",
			http.StatusBadRequest,
		)

		return
	}

	connectedNodes :=
		getConnectedExplorerNodes(
			h.Nodes.List(),
		)

	node :=
		findExplorerNode(
			connectedNodes,
			nodeID,
		)

	if node == nil {
		http.Error(
			w,
			"selected node is not connected",
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

	ctx, cancel :=
		context.WithCancel(
			r.Context(),
		)

	defer cancel()

	/*
	 * Explorer now streams exactly one node.
	 *
	 * The collector already batches up to 100
	 * records, so those batches can pass directly
	 * into GetFindings.
	 */
	batches := make(
		chan log.StreamLogBatch,
		2,
	)

	go func() {
		defer close(
			batches,
		)

		log.StreamCollectorLogs(
			ctx,
			node,
			params,
			batches,
		)
	}()

	totalMatches := 0
	completedNodes := 0
	failedNodes := 0

	streamFailed := false

	for batch := range batches {

		if batch.Err != nil {
			failedNodes = 1
			streamFailed = true

			if err :=
				writeSSE(
					w,
					"node_error",
					explorerSSEError{
						NodeID: node.ID,

						NodeName: node.Name,

						Message: batch.Err.Error(),
					},
				); err != nil {

				return
			}

			flusher.Flush()

			break
		}

		if batch.Done {
			completedNodes = 1

			if err :=
				writeSSE(
					w,
					"node_done",
					map[string]string{
						"node_id": node.ID,

						"node_name": node.Name,
					},
				); err != nil {

				return
			}

			flusher.Flush()

			break
		}

		if len(batch.Logs) == 0 {
			continue
		}

		/*
		 * The whole batch belongs to the selected
		 * node, so findings can be looked up directly
		 * without regrouping anything.
		 */
		if err :=
			batch.Logs.GetFindings(
				ctx,
				h.DB,
				h.Config.FindingsLog,
				node.ID,
				params.Start.UnixNano(),
				params.End.UnixNano(),
			); err != nil {

			failedNodes = 1
			streamFailed = true

			if err :=
				writeSSE(
					w,
					"node_error",
					explorerSSEError{
						NodeID: node.ID,

						NodeName: node.Name,

						Message: fmt.Sprintf(
							"failed to retrieve findings: %v",
							err,
						),
					},
				); err != nil {

				return
			}

			flusher.Flush()

			break
		}

		results := make(
			[]explorerSSELog,
			0,
			len(batch.Logs),
		)

		for _, item := range batch.Logs {

			matches, err :=
				query.EvaluateExpression(
					params.Query,
					queryExpression,
					item,
				)

			if err != nil {
				failedNodes = 1
				streamFailed = true

				if err :=
					writeSSE(
						w,
						"node_error",
						explorerSSEError{
							NodeID: node.ID,

							NodeName: node.Name,

							Message: fmt.Sprintf(
								"failed to evaluate query: %v",
								err,
							),
						},
					); err != nil {

					return
				}

				flusher.Flush()

				break
			}

			if !matches {
				continue
			}

			result, err :=
				logItemToSSE(
					item,
				)

			if err != nil {
				continue
			}

			result.NodeID =
				node.ID

			result.NodeName =
				node.Name

			results =
				append(
					results,
					result,
				)
		}

		if streamFailed {
			break
		}

		if len(results) == 0 {
			continue
		}

		totalMatches +=
			len(results)

		/*
		 * Send the node's complete filtered batch.
		 *
		 * No global merge is necessary anymore.
		 */
		if err :=
			writeSSE(
				w,
				"logs",
				results,
			); err != nil {

			return
		}

		flusher.Flush()
	}

	_ = writeSSE(
		w,
		"done",
		map[string]int{
			"matches": totalMatches,

			"completed_nodes": completedNodes,

			"failed_nodes": failedNodes,
		},
	)

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
