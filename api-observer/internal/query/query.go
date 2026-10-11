// Package query handles querying operations for API Observer's dashboard.
package query

import (
	"api-observer/internal/audit"
	queryv1 "api-observer/proto/query/v1"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type LogItems []LogItem

type LogItem struct {
	Log      *queryv1.Log
	Findings []audit.Finding
}

func (items *LogItems) GetFindings(
	ctx context.Context,
	db *sql.DB,
	findingsPath string,
	nodeID string,
	startTime int64,
	endTime int64,
) error {
	itemIndexes := make(
		map[string][]int,
	)

	for i := range *items {
		item := &(*items)[i]

		if item.Log == nil {
			continue
		}

		switch event := item.Log.Event.(type) {
		case *queryv1.Log_Request:
			if event.Request == nil ||
				event.Request.Metadata == nil {

				continue
			}

			requestID :=
				event.Request.Metadata.RequestId

			if requestID == "" {
				continue
			}

			itemIndexes[requestID] = append(
				itemIndexes[requestID],
				i,
			)

		case *queryv1.Log_Response:
			if event.Response == nil ||
				event.Response.Metadata == nil {

				continue
			}

			requestID :=
				event.Response.Metadata.RequestId

			if requestID == "" {
				continue
			}

			itemIndexes[requestID] = append(
				itemIndexes[requestID],
				i,
			)
		}
	}

	rows, err := db.QueryContext(
		ctx,
		`
	SELECT
		to_json(t)
	FROM read_json(
		?,
		format = 'newline_delimited',
		auto_detect = false,
		columns = {
			title: 'VARCHAR',
			message: 'VARCHAR',
			severity: 'VARCHAR',
			tags: 'VARCHAR[]',
			metadata: 'STRUCT(
				request STRUCT(
					method VARCHAR,
					url JSON,
					headers JSON,
					body VARCHAR,
					content_length BIGINT,
					metadata STRUCT(
						request_id VARCHAR,
						source VARCHAR,
						timestamp BIGINT
					)
				),
				response STRUCT(
					status_code INTEGER,
					headers JSON,
					body VARCHAR,
					content_length BIGINT,
					metadata STRUCT(
						request_id VARCHAR,
						source VARCHAR,
						timestamp BIGINT
					)
				)
			)',
			processed_at: 'VARCHAR'
		}
	) AS t
	WHERE
		COALESCE(
			t.metadata.request.metadata.source,
			t.metadata.response.metadata.source
		) = ?
		AND COALESCE(
			t.metadata.request.metadata.timestamp,
			t.metadata.response.metadata.timestamp
		) BETWEEN ? AND ?
	`,
		findingsPath,
		nodeID,
		startTime,
		endTime,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to query findings log: %w",
			err,
		)
	}
	defer rows.Close()

	for rows.Next() {
		var raw any

		if err := rows.Scan(
			&raw,
		); err != nil {
			return fmt.Errorf(
				"failed to scan finding: %w",
				err,
			)
		}

		data, err := json.Marshal(
			raw,
		)
		if err != nil {
			return fmt.Errorf(
				"failed to marshal DuckDB finding result: %w",
				err,
			)
		}

		var finding audit.Finding

		if err := json.Unmarshal(
			data,
			&finding,
		); err != nil {
			return fmt.Errorf(
				"failed to unmarshal finding: %w",
				err,
			)
		}

		requestID := ""

		if finding.Metadata.Request != nil {
			requestID =
				finding.Metadata.Request.Metadata.RequestID
		} else if finding.Metadata.Response != nil {
			requestID =
				finding.Metadata.Response.Metadata.RequestID
		}

		if requestID == "" {
			continue
		}

		indexes, ok :=
			itemIndexes[requestID]
		if !ok {
			continue
		}

		for _, index := range indexes {
			item :=
				&(*items)[index]

			item.Findings = append(
				item.Findings,
				finding,
			)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf(
			"failed while reading findings query results: %w",
			err,
		)
	}

	return nil
}

func ParseQuery(
	rawString string,
) (Expression, error) {
	if len(rawString) == 0 {
		return nil, nil
	}

	l := Lexer{
		Query:    rawString,
		Position: 0,
		Tokens:   make([]Token, 0),
	}

	if err := l.Process(); err != nil {
		return nil, err
	}

	p := Parser{
		Query:         rawString,
		Tokens:        l.Tokens,
		TokenPosition: 0,
	}

	expr, err := p.Parse()
	if err != nil {
		return nil, err
	}

	return expr, nil
}
