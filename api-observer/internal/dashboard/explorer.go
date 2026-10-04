package dashboard

import (
	"api-observer/internal/dashboard/views/explorer"
	queryv1 "api-observer/proto/query/v1"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
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

	var (
		logs       []*queryv1.Log
		nextCursor int64
		hasMore    bool
	)

	if params.NodeID != "" {
		node, ok := h.Nodes.Get(
			params.NodeID,
		)
		if !ok {
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

		// TODO: Add the query string to streamLogs and filter out logs that are not needed.
		logs, nextCursor, hasMore, err = streamLogs(
			r.Context(),
			node.Client,
			params.Query,
			params.Start,
			params.End,
			params.Cursor,
			params.Limit,
		)
		if err != nil {
			http.Error(
				w,
				fmt.Sprintf(
					"failed to retrieve logs: %v",
					err,
				),
				http.StatusInternalServerError,
			)

			return
		}

		_ = params.Query
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

	node, ok := h.Nodes.Get(
		params.NodeID,
	)
	if !ok {
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

	logs, nextCursor, hasMore, err := streamLogs(
		r.Context(),
		node.Client,
		params.Query,
		params.Start,
		params.End,
		params.Cursor,
		params.Limit,
	)
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf(
				"failed to retrieve logs: %v",
				err,
			),
			http.StatusInternalServerError,
		)

		return
	}

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

	/*
		Cursor.

		The normal /explorer request usually has no cursor.

		HTMX infinite-scroll requests will provide one.
	*/
	cursorString := queries.Get("cursor")

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

	/*
		Limit.
	*/
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

	/*
		Default to the most recent hour.

		These strings are also passed back to the datetime-local
		inputs so the form always reflects the active query.
	*/
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

	if params.End.Before(params.Start) {
		return explorerParams{},
			fmt.Errorf(
				"end time must be after start time",
			)
	}

	return params, nil
}

func streamLogs(
	ctx context.Context,
	client queryv1.LogServiceClient,
	queryString string,
	start time.Time,
	end time.Time,
	startCursor int64,
	limit int,
) (
	[]*queryv1.Log,
	int64,
	bool,
	error,
) {
	queryCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := client.Query(
		queryCtx,
		&queryv1.LogParams{
			StartDate: timestamppb.New(start),
			EndDate:   timestamppb.New(end),
			Cursor:    startCursor,
		},
	)
	if err != nil {
		return nil,
			startCursor,
			false,
			fmt.Errorf(
				"failed to query node: %w",
				err,
			)
	}

	logs := make(
		[]*queryv1.Log,
		0,
		limit,
	)

	nextCursor := startCursor

	for {
		logRecord, err := stream.Recv()

		if err == io.EOF {
			return logs,
				nextCursor,
				false,
				nil
		}

		if err != nil {
			return logs,
				nextCursor,
				false,
				fmt.Errorf(
					"failed to receive log: %w",
					err,
				)
		}

		// TODO: Filter the streamed data using the query engine before appending to logs

		logs = append(
			logs,
			logRecord,
		)

		switch logRecord.Event.(type) {
		case *queryv1.Log_Response:
			nextCursor = logRecord.Cursor
		}

		if limit > 0 &&
			len(logs) >= limit {

			switch logRecord.Event.(type) {
			case *queryv1.Log_Response:
				cancel()

				return logs,
					nextCursor,
					true,
					nil
			}
		}
	}
}

func parseExplorerTime(
	value string,
) (time.Time, error) {
	/*
		Allow RFC3339 so this endpoint can also be called
		manually or by non-browser clients.
	*/
	if parsed, err := time.Parse(
		time.RFC3339,
		value,
	); err == nil {
		return parsed, nil
	}

	/*
		HTML datetime-local submits values like:

			2026-09-30T19:18

		There is no timezone encoded by datetime-local, so use
		the server's local timezone.
	*/
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
