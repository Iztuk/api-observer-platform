// Package query handles querying operations for API Observer's dashboard.
package query

import (
	"api-observer/internal/audit"
	ingestv1 "api-observer/proto/ingest/v1"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type LogItems []LogItem

type LogItem struct {
	Log      *ingestv1.Record
	Findings []audit.Finding
	NodeName string
}

func (items *LogItems) First() *LogItem {
	if items == nil || len(*items) == 0 {
		return nil
	}

	return &(*items)[0]
}

func (items *LogItems) Last() *LogItem {
	if items == nil || len(*items) == 0 {
		return nil
	}

	return &(*items)[len(*items)-1]
}

func (items *LogItems) GetFindings(
	ctx context.Context,
	db *sql.DB,
	findingsPath string,
) error {
	if items == nil || len(*items) == 0 {
		return nil
	}

	type logKey struct {
		RequestID string
		Type      audit.JobType
	}

	itemIndexes := make(map[logKey][]int)

	var startTime int64
	var endTime int64

	for i := range *items {
		item := &(*items)[i]

		if item.Log == nil {
			continue
		}

		switch event := item.Log.Event.(type) {
		case *ingestv1.Record_Request:
			if event.Request == nil || event.Request.Metadata == nil {
				continue
			}

			requestID := event.Request.Metadata.RequestId

			if requestID != "" {
				key := logKey{
					RequestID: requestID,
					Type:      audit.JobTypeRequest,
				}

				itemIndexes[key] = append(itemIndexes[key], i)
			}

		case *ingestv1.Record_Response:
			if event.Response == nil || event.Response.Metadata == nil {
				continue
			}

			requestID := event.Response.Metadata.RequestId

			if requestID != "" {
				key := logKey{
					RequestID: requestID,
					Type:      audit.JobTypeResponse,
				}

				itemIndexes[key] = append(itemIndexes[key], i)
			}
		}
	}

	firstTimestamp, err := logItemTimestamp(*items.First())
	if err != nil {
		return fmt.Errorf("failed to get first log timestamp: %w", err)
	}

	lastTimestamp, err := logItemTimestamp(*items.Last())
	if err != nil {
		return fmt.Errorf("failed to get last log timestamp: %w", err)
	}

	if firstTimestamp < lastTimestamp {
		startTime = firstTimestamp
		endTime = lastTimestamp
	} else {
		startTime = lastTimestamp
		endTime = firstTimestamp
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
			t.metadata.request.metadata.timestamp,
			t.metadata.response.metadata.timestamp
		) BETWEEN ? AND ?
	`,
		findingsPath,
		startTime,
		endTime,
	)
	if err != nil {
		return fmt.Errorf("failed to query findings log: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var raw any

		if err := rows.Scan(&raw); err != nil {
			return fmt.Errorf("failed to scan finding: %w", err)
		}

		data, err := json.Marshal(raw)
		if err != nil {
			return fmt.Errorf("failed to marshal DuckDB finding result: %w", err)
		}

		var finding audit.Finding

		if err := json.Unmarshal(data, &finding); err != nil {
			return fmt.Errorf("failed to unmarshal finding: %w", err)
		}

		if finding.Metadata.Request != nil {
			requestID := finding.Metadata.Request.Metadata.RequestID

			if requestID != "" {
				key := logKey{
					RequestID: requestID,
					Type:      audit.JobTypeRequest,
				}

				for _, index := range itemIndexes[key] {
					item := &(*items)[index]
					item.Findings = append(item.Findings, finding)
				}
			}
		}

		if finding.Metadata.Response != nil {
			requestID := finding.Metadata.Response.Metadata.RequestID

			if requestID != "" {
				key := logKey{
					RequestID: requestID,
					Type:      audit.JobTypeResponse,
				}

				for _, index := range itemIndexes[key] {
					item := &(*items)[index]
					item.Findings = append(item.Findings, finding)
				}
			}
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed while reading findings query results: %w", err)
	}

	return nil
}

func logItemTimestamp(item LogItem) (int64, error) {
	if item.Log == nil {
		return 0, fmt.Errorf("log item is nil")
	}

	switch event := item.Log.Event.(type) {
	case *ingestv1.Record_Request:
		if event.Request == nil ||
			event.Request.Metadata == nil ||
			event.Request.Metadata.Timestamp == nil {
			return 0, fmt.Errorf("request timestamp is missing")
		}

		return event.Request.Metadata.Timestamp.AsTime().UnixNano(), nil

	case *ingestv1.Record_Response:
		if event.Response == nil ||
			event.Response.Metadata == nil ||
			event.Response.Metadata.Timestamp == nil {
			return 0, fmt.Errorf("response timestamp is missing")
		}

		return event.Response.Metadata.Timestamp.AsTime().UnixNano(), nil

	default:
		return 0, fmt.Errorf("unsupported log event type %T", item.Log.Event)
	}
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
