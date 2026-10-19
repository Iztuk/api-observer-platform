package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"api-observer/internal/audit"
	"api-observer/internal/dashboard/views/analysis"
	"api-observer/internal/log"
	"api-observer/internal/nodes"
	"api-observer/internal/query"
	ingestv1 "api-observer/proto/ingest/v1"
)

const (
	analysisSSEBatchSize = 50
)

type analysisSSELog struct {
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`

	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Method    string `json:"method,omitempty"`
	Path      string `json:"path,omitempty"`
	Status    int32  `json:"status,omitempty"`

	Findings []audit.Finding `json:"findings"`
}

type analysisSSEError struct {
	NodeID   string `json:"node_id,omitempty"`
	NodeName string `json:"node_name,omitempty"`
	Message  string `json:"message"`
}

func (h *Handler) AnalysisPage(
	w http.ResponseWriter,
	r *http.Request,
) {
	params, err :=
		parseAnalysisParams(
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

	if err :=
		analysis.AnalysisPage(
			"Analysis",
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

func (h *Handler) AnalysisLogs(
	w http.ResponseWriter,
	r *http.Request,
) {
	params, err :=
		parseAnalysisParams(
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

	ruleSet, err :=
		parseAnalysisRuleSet(
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

	ctx, cancel :=
		context.WithCancel(
			r.Context(),
		)

	defer cancel()

	batches := make(
		chan log.StreamLogBatch,
		len(targetNodes)*2,
	)

	var wg sync.WaitGroup

	for _, node := range targetNodes {

		wg.Add(1)

		go func(
			node *nodes.Node,
		) {
			defer wg.Done()

			log.StreamCollectorLogs(
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
		[]analysisSSELog,
		0,
		analysisSSEBatchSize,
	)

	flushSSEBatch :=
		func() error {
			if len(sseBatch) == 0 {
				return nil
			}

			if err :=
				writeAnalysisSSE(
					w,
					"logs",
					sseBatch,
				); err != nil {

				return err
			}

			flusher.Flush()

			sseBatch =
				sseBatch[:0]

			return nil
		}

	for batch := range batches {

		if batch.Err != nil {
			failedNodes++

			if err :=
				writeAnalysisSSE(
					w,
					"node_error",
					analysisSSEError{
						NodeID: batch.NodeID,

						NodeName: batch.NodeName,

						Message: batch.Err.Error(),
					},
				); err != nil {

				return
			}

			flusher.Flush()

			continue
		}

		if batch.Done {
			completedNodes++

			if err :=
				writeAnalysisSSE(
					w,
					"node_done",
					map[string]string{
						"node_id": batch.NodeID,

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

		for _, item := range batch.Logs {

			job, err :=
				analysisLogToJob(
					item.Log,
				)

			if err != nil {
				if err :=
					writeAnalysisSSE(
						w,
						"node_error",
						analysisSSEError{
							NodeID: batch.NodeID,

							NodeName: batch.NodeName,

							Message: fmt.Sprintf(
								"failed to create audit job: %v",
								err,
							),
						},
					); err != nil {

					return
				}

				flusher.Flush()

				continue
			}

			findings, err :=
				ruleSet.Evaluate(
					job,
				)

			if err != nil {
				if err :=
					writeAnalysisSSE(
						w,
						"node_error",
						analysisSSEError{
							NodeID: batch.NodeID,

							NodeName: batch.NodeName,

							Message: fmt.Sprintf(
								"failed to evaluate rule set: %v",
								err,
							),
						},
					); err != nil {

					return
				}

				flusher.Flush()

				continue
			}

			item.Findings =
				append(
					item.Findings,
					findings...,
				)

			matches, err :=
				query.EvaluateExpression(
					params.Query,
					queryExpression,
					item,
				)

			if err != nil {
				if err :=
					writeAnalysisSSE(
						w,
						"node_error",
						analysisSSEError{
							NodeID: batch.NodeID,

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

			result, err :=
				analysisLogItemToSSE(
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
				analysisSSEBatchSize {

				if err :=
					flushSSEBatch(); err != nil {

					return
				}
			}
		}
	}

	if err :=
		flushSSEBatch(); err != nil {

		return
	}

	if err :=
		writeAnalysisSSE(
			w,
			"done",
			map[string]int{
				"matches": totalMatches,

				"completed_nodes": completedNodes,

				"failed_nodes": failedNodes,
			},
		); err != nil {

		return
	}

	flusher.Flush()
}

func parseAnalysisRuleSet(
	r *http.Request,
) (*audit.RuleSet, error) {
	rules := r.FormValue("rules")

	if rules == "" {
		return nil,
			fmt.Errorf(
				"rule set is required",
			)
	}

	return audit.ParseRuleSet(rules)
}

func writeAnalysisSSE(
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

func analysisLogItemToSSE(
	item query.LogItem,
) (analysisSSELog, error) {
	if item.Log == nil {
		return analysisSSELog{},
			fmt.Errorf(
				"log item is nil",
			)
	}

	switch event :=
		item.Log.Event.(type) {

	case *ingestv1.Record_Request:
		if event.Request == nil {
			return analysisSSELog{},
				fmt.Errorf(
					"request is nil",
				)
		}

		result :=
			analysisSSELog{
				NodeName: item.NodeName,

				Type: "Request",

				Method: event.Request.Method,

				Path: event.Request.Url,

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

	case *ingestv1.Record_Response:
		if event.Response == nil {
			return analysisSSELog{},
				fmt.Errorf(
					"response is nil",
				)
		}

		result :=
			analysisSSELog{
				NodeName: item.NodeName,

				Type: "Response",

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
		return analysisSSELog{},
			fmt.Errorf(
				"unsupported log event type %T",
				item.Log.Event,
			)
	}
}

func analysisLogToJob(
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

					Headers: analysisToHTTPHeader(
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

					Headers: analysisToHTTPHeader(
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

	default:
		return audit.Job{},
			fmt.Errorf(
				"unsupported log event type %T",
				logRecord.Event,
			)
	}
}

func analysisToHTTPHeader(
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

func parseAnalysisParams(
	r *http.Request,
) (log.StreamParams, error) {
	params :=
		log.StreamParams{
			Query: r.FormValue(
				"query",
			),

			StartString: r.FormValue(
				"start",
			),

			EndString: r.FormValue(
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
			parseAnalysisTime(
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
			parseAnalysisTime(
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

	return params, nil
}

func parseAnalysisTime(
	value string,
) (time.Time, error) {
	if parsed, err :=
		time.Parse(
			time.RFC3339,
			value,
		); err == nil {

		return parsed, nil
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

	return parsed, nil
}
