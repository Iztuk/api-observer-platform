// Package ingest handles the ingestion of logs.
package ingest

import (
	"errors"
	"io"

	"api-observer/internal/audit"
	ingestv1 "api-observer/proto/ingest/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	ingestv1.UnimplementedIngestServiceServer

	queue *audit.Queue
}

func NewServer(
	queue *audit.Queue,
) *Server {
	return &Server{
		queue: queue,
	}
}

func (s *Server) Ingest(
	stream ingestv1.IngestService_IngestServer,
) error {
	for {
		record, err := stream.Recv()

		if errors.Is(
			err,
			io.EOF,
		) {
			/*
				The client has finished sending records.
			*/
			return nil
		}

		if err != nil {
			return err
		}

		if record == nil {
			return status.Error(
				codes.InvalidArgument,
				"received nil record",
			)
		}

		/*
			Get the request ID from the event metadata.

			Request and Response records both carry their
			identity in Metadata.
		*/
		recordID, err := recordID(
			record,
		)
		if err != nil {
			if sendErr := stream.Send(
				&ingestv1.IngestAck{
					Status: ingestv1.IngestStatus_INGEST_STATUS_REJECTED,

					Message: err.Error(),

					Retryable: false,
				},
			); sendErr != nil {
				return sendErr
			}

			continue
		}

		/*
			Convert the protobuf record into the internal
			audit.Job representation.
		*/
		job, err := toAuditJob(
			record,
		)
		if err != nil {
			if sendErr := stream.Send(
				&ingestv1.IngestAck{
					RecordId: recordID,

					Status: ingestv1.IngestStatus_INGEST_STATUS_REJECTED,

					Message: err.Error(),

					Retryable: false,
				},
			); sendErr != nil {
				return sendErr
			}

			continue
		}

		/*
			TryEnqueue is non-blocking.

			If the queue is currently full, tell the
			collector that this record can be retried.
		*/
		if !s.queue.TryEnqueue(
			job,
		) {
			if sendErr := stream.Send(
				&ingestv1.IngestAck{
					RecordId: recordID,

					Status: ingestv1.IngestStatus_INGEST_STATUS_REJECTED,

					Message: "queue is full or unavailable",

					Retryable: true,
				},
			); sendErr != nil {
				return sendErr
			}

			continue
		}

		/*
			The record was successfully accepted into the
			audit processing queue.
		*/
		if err := stream.Send(
			&ingestv1.IngestAck{
				RecordId: recordID,

				Status: ingestv1.IngestStatus_INGEST_STATUS_ACCEPTED,

				Message: "record accepted",

				Retryable: false,
			},
		); err != nil {
			return err
		}
	}
}

func recordID(
	record *ingestv1.Record,
) (string, error) {
	if record == nil {
		return "",
			status.Error(
				codes.InvalidArgument,
				"record is nil",
			)
	}

	switch event := record.Event.(type) {
	case *ingestv1.Record_Request:
		if event.Request == nil {
			return "",
				status.Error(
					codes.InvalidArgument,
					"request is nil",
				)
		}

		if event.Request.Metadata == nil {
			return "",
				status.Error(
					codes.InvalidArgument,
					"request metadata is missing",
				)
		}

		if event.Request.Metadata.RequestId == "" {
			return "",
				status.Error(
					codes.InvalidArgument,
					"request ID is missing",
				)
		}

		return event.Request.Metadata.RequestId,
			nil

	case *ingestv1.Record_Response:
		if event.Response == nil {
			return "",
				status.Error(
					codes.InvalidArgument,
					"response is nil",
				)
		}

		if event.Response.Metadata == nil {
			return "",
				status.Error(
					codes.InvalidArgument,
					"response metadata is missing",
				)
		}

		if event.Response.Metadata.RequestId == "" {
			return "",
				status.Error(
					codes.InvalidArgument,
					"request ID is missing",
				)
		}

		return event.Response.Metadata.RequestId,
			nil

	default:
		return "",
			status.Error(
				codes.InvalidArgument,
				"record does not contain a request or response",
			)
	}
}
