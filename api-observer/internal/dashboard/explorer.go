package dashboard

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"api-observer/internal/audit"
	"api-observer/internal/dashboard/views/explorer"
	"api-observer/internal/query"
	queryv1 "api-observer/proto/query/v1"
)

const (
	explorerDateTimeLayout = "2006-01-02T15:04"
	defaultExplorerLimit   = 100
)

type explorerParams struct {
	NodeID string
	Query  string

	Cursor int64
	Limit  int

	Start time.Time
	End   time.Time

	StartString string
	EndString   string
	LimitString string
}

func (h *Handler) ExplorerPage(
	w http.ResponseWriter,
	r *http.Request,
) {
	params, err := parseExplorerParams(r)
	if err != nil {
		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)

		return
	}

	nodeList := h.Nodes.List()

	logs := make(
		query.LogItems,
		0,
	)

	var nextCursor int64
	var hasMore bool

	if params.NodeID != "" {
		if _, ok := h.Nodes.Get(
			params.NodeID,
		); !ok {
			http.Error(
				w,
				fmt.Sprintf(
					"node %q not found",
					params.NodeID,
				),
				http.StatusNotFound,
			)

			return
		}
	}

	if err := explorer.ExplorerPage(
		"Explorer",
		params.Query,
		params.StartString,
		params.EndString,
		params.NodeID,
		params.LimitString,
		nodeList,
		logs,
		nextCursor,
		hasMore,
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
	params, err := parseExplorerParams(r)
	if err != nil {
		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)

		return
	}

	if params.NodeID == "" {
		http.Error(
			w,
			"node is required",
			http.StatusBadRequest,
		)

		return
	}

	if _, ok := h.Nodes.Get(
		params.NodeID,
	); !ok {
		http.Error(
			w,
			fmt.Sprintf(
				"node %q not found",
				params.NodeID,
			),
			http.StatusNotFound,
		)

		return
	}

	logs := make(
		query.LogItems,
		0,
	)

	var nextCursor int64
	var hasMore bool

	if err := explorer.ExplorerLogRows(
		logs,
		nextCursor,
		hasMore,
	).Render(
		r.Context(),
		w,
	); err != nil {
		http.Error(
			w,
			"failed to render logs",
			http.StatusInternalServerError,
		)

		return
	}
}

func parseExplorerParams(
	r *http.Request,
) (explorerParams, error) {
	queries := r.URL.Query()

	params := explorerParams{
		NodeID: queries.Get("node"),
		Query:  queries.Get("query"),

		StartString: queries.Get("start"),
		EndString:   queries.Get("end"),
		LimitString: queries.Get("limit"),

		Limit: defaultExplorerLimit,
	}

	cursorString := queries.Get(
		"cursor",
	)

	if cursorString != "" {
		cursor, err := strconv.ParseInt(
			cursorString,
			10,
			64,
		)
		if err != nil || cursor < 0 {
			return explorerParams{},
				fmt.Errorf(
					"invalid cursor",
				)
		}

		params.Cursor = cursor
	}

	if params.LimitString == "" {
		params.LimitString = strconv.Itoa(
			defaultExplorerLimit,
		)
	} else {
		limit, err := strconv.Atoi(
			params.LimitString,
		)
		if err != nil || limit <= 0 {
			return explorerParams{},
				fmt.Errorf(
					"invalid limit",
				)
		}

		params.Limit = limit
	}

	now := time.Now()

	if params.EndString == "" {
		params.End = now

		params.EndString = params.End.Format(
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
		params.Start = params.End.Add(
			-1 * time.Hour,
		)

		params.StartString = params.Start.Format(
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
			job.Request.Metadata = audit.Metadata{
				RequestID: request.Metadata.RequestId,
				Source:    request.Metadata.Source,
			}

			if request.Metadata.Timestamp != nil {
				job.Request.Metadata.Timestamp =
					request.Metadata.Timestamp.
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
			job.Response.Metadata = audit.Metadata{
				RequestID: response.Metadata.RequestId,
				Source:    response.Metadata.Source,
			}

			if response.Metadata.Timestamp != nil {
				job.Response.Metadata.Timestamp =
					response.Metadata.Timestamp.
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
