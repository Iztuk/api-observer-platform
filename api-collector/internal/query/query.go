// Package query handles querying operations from API Observer.
package query

import (
	"database/sql"
	"fmt"
	"time"

	"api-collector/internal/watcher"
	ingestv1 "api-collector/proto/ingest/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	ingestv1.UnimplementedIngestServiceServer

	db *sql.DB

	filePath    string
	collectorID string
}

func NewServer(
	db *sql.DB,
	filePath string,
	collectorID string,
) *Server {
	return &Server{
		db:          db,
		filePath:    filePath,
		collectorID: collectorID,
	}
}

func (s *Server) Logs(
	req *ingestv1.LogRequest,
	stream ingestv1.IngestService_LogsServer,
) error {
	if err := validateLogRequest(
		req,
	); err != nil {
		return err
	}

	if s.db == nil {
		return status.Error(
			codes.FailedPrecondition,
			"DuckDB is not initialized",
		)
	}

	if s.filePath == "" {
		return status.Error(
			codes.FailedPrecondition,
			"log file path is empty",
		)
	}

	startTime := req.StartDate.
		AsTime().
		UTC()

	endTime := req.EndDate.
		AsTime().
		UTC()

	queryString := `
		WITH logs AS (
			SELECT
				CAST(json AS VARCHAR) AS raw_json,

				try_strptime(
					json_extract_string(
						json,
						'$.transaction.time_stamp'
					),
					[
						'%a %b %d %H:%M:%S %Y',
						'%a %b %-d %H:%M:%S %Y'
					]
				) AS log_timestamp,

				json_extract_string(
					json,
					'$.transaction.unique_id'
				) AS request_id

			FROM read_ndjson_objects(?)
		)

		SELECT
			raw_json,
			log_timestamp,
			request_id

		FROM logs

		WHERE
			log_timestamp IS NOT NULL
			AND request_id IS NOT NULL
			AND log_timestamp >= ?
			AND log_timestamp <= ?
	`

	args := []any{
		s.filePath,
		startTime,
		endTime,
	}

	if req.Cursor != nil {
		cursorTime := req.Cursor.Timestamp.
			AsTime().
			UTC()

		queryString += `
			AND (
				log_timestamp < ?
				OR (
					log_timestamp = ?
					AND request_id < ?
				)
			)
		`

		args = append(
			args,
			cursorTime,
			cursorTime,
			req.Cursor.RequestId,
		)
	}

	queryString += `
		ORDER BY
			log_timestamp DESC,
			request_id DESC
	`

	rows, err := s.db.QueryContext(
		stream.Context(),
		queryString,
		args...,
	)
	if err != nil {
		return status.Errorf(
			codes.Internal,
			"failed to query ModSecurity logs: %v",
			err,
		)
	}
	defer rows.Close()

	for rows.Next() {
		if err := stream.Context().Err(); err != nil {
			return err
		}

		var (
			rawJSON      string
			logTimestamp time.Time
			requestID    string
		)

		if err := rows.Scan(
			&rawJSON,
			&logTimestamp,
			&requestID,
		); err != nil {
			return status.Errorf(
				codes.Internal,
				"failed to scan ModSecurity log: %v",
				err,
			)
		}

		request, response, err :=
			watcher.DecodeModSecLog(
				[]byte(rawJSON),
				s.collectorID,
			)
		if err != nil {
			return status.Errorf(
				codes.Internal,
				"failed to decode ModSecurity transaction %q: %v",
				requestID,
				err,
			)
		}

		transaction := &ingestv1.LogTransaction{
			Request:  request,
			Response: response,

			Cursor: &ingestv1.LogCursor{
				Timestamp: timestamppb.New(
					logTimestamp.UTC(),
				),

				RequestId: requestID,
			},
		}

		if err := stream.Send(
			transaction,
		); err != nil {
			return fmt.Errorf(
				"failed to stream transaction %q: %w",
				requestID,
				err,
			)
		}
	}

	if err := rows.Err(); err != nil {
		return status.Errorf(
			codes.Internal,
			"failed while reading ModSecurity logs: %v",
			err,
		)
	}

	return nil
}

func validateLogRequest(
	req *ingestv1.LogRequest,
) error {
	if req == nil {
		return status.Error(
			codes.InvalidArgument,
			"log request is nil",
		)
	}

	if req.StartDate == nil {
		return status.Error(
			codes.InvalidArgument,
			"start date is required",
		)
	}

	if req.EndDate == nil {
		return status.Error(
			codes.InvalidArgument,
			"end date is required",
		)
	}

	if err := req.StartDate.CheckValid(); err != nil {
		return status.Errorf(
			codes.InvalidArgument,
			"invalid start date: %v",
			err,
		)
	}

	if err := req.EndDate.CheckValid(); err != nil {
		return status.Errorf(
			codes.InvalidArgument,
			"invalid end date: %v",
			err,
		)
	}

	startTime := req.StartDate.AsTime()
	endTime := req.EndDate.AsTime()

	if endTime.Before(
		startTime,
	) {
		return status.Error(
			codes.InvalidArgument,
			"end date must be after start date",
		)
	}

	if req.Cursor != nil {
		if req.Cursor.Timestamp == nil {
			return status.Error(
				codes.InvalidArgument,
				"cursor timestamp is required",
			)
		}

		if err := req.Cursor.Timestamp.CheckValid(); err != nil {
			return status.Errorf(
				codes.InvalidArgument,
				"invalid cursor timestamp: %v",
				err,
			)
		}

		if req.Cursor.RequestId == "" {
			return status.Error(
				codes.InvalidArgument,
				"cursor request ID is required",
			)
		}
	}

	return nil
}
