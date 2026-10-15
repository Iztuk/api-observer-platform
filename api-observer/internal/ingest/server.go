// Package ingest handles the ingestion of logs.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"api-observer/internal/audit"
	"api-observer/internal/nodes"
	ingestv1 "api-observer/proto/ingest/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type Server struct {
	ingestv1.UnimplementedIngestServiceServer

	queue       *audit.Queue
	nodeManager *nodes.NodeManager
}

func NewServer(
	queue *audit.Queue,
	nodeManager *nodes.NodeManager,
) *Server {
	return &Server{
		queue:       queue,
		nodeManager: nodeManager,
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

		switch event := record.Event.(type) {
		case *ingestv1.Record_Heartbeat:
			if err := s.handleHeartbeat(
				stream.Context(),
				event.Heartbeat,
			); err != nil {
				if sendErr := stream.Send(
					&ingestv1.IngestAck{
						Status:    ingestv1.IngestStatus_INGEST_STATUS_REJECTED,
						Message:   err.Error(),
						Retryable: false,
					},
				); sendErr != nil {
					return sendErr
				}

				continue
			}

			if err := stream.Send(
				&ingestv1.IngestAck{
					RecordId:  event.Heartbeat.CollectorId,
					Status:    ingestv1.IngestStatus_INGEST_STATUS_ACCEPTED,
					Message:   "heartbeat accepted",
					Retryable: false,
				},
			); err != nil {
				return err
			}

		default:
			recordID, err := recordID(
				record,
			)
			if err != nil {
				if sendErr := stream.Send(
					&ingestv1.IngestAck{
						Status:    ingestv1.IngestStatus_INGEST_STATUS_REJECTED,
						Message:   err.Error(),
						Retryable: false,
					},
				); sendErr != nil {
					return sendErr
				}

				continue
			}

			job, err := toAuditJob(
				record,
			)
			if err != nil {
				if sendErr := stream.Send(
					&ingestv1.IngestAck{
						RecordId:  recordID,
						Status:    ingestv1.IngestStatus_INGEST_STATUS_REJECTED,
						Message:   err.Error(),
						Retryable: false,
					},
				); sendErr != nil {
					return sendErr
				}

				continue
			}

			if !s.queue.TryEnqueue(
				job,
			) {
				if sendErr := stream.Send(
					&ingestv1.IngestAck{
						RecordId:  recordID,
						Status:    ingestv1.IngestStatus_INGEST_STATUS_REJECTED,
						Message:   "queue is full or unavailable",
						Retryable: true,
					},
				); sendErr != nil {
					return sendErr
				}

				continue
			}

			if err := stream.Send(
				&ingestv1.IngestAck{
					RecordId:  recordID,
					Status:    ingestv1.IngestStatus_INGEST_STATUS_ACCEPTED,
					Message:   "record accepted",
					Retryable: false,
				},
			); err != nil {
				return err
			}
		}
	}
}

func (s *Server) handleHeartbeat(
	ctx context.Context,
	heartbeat *ingestv1.Heartbeat,
) error {
	if heartbeat == nil {
		return fmt.Errorf(
			"heartbeat is nil",
		)
	}

	if heartbeat.CollectorId == "" {
		return fmt.Errorf(
			"collector ID is missing",
		)
	}

	if s.nodeManager == nil {
		return fmt.Errorf(
			"node manager is unavailable",
		)
	}

	addr := ""

	if p, ok := peer.FromContext(
		ctx,
	); ok && p.Addr != nil {
		addr = p.Addr.String()
	}

	_, exists := s.nodeManager.Get(
		heartbeat.CollectorId,
	)

	if !exists {
		if err := s.nodeManager.Add(
			heartbeat.CollectorId,
			addr,
		); err != nil {
			return fmt.Errorf(
				"failed to register collector %q: %w",
				heartbeat.CollectorId,
				err,
			)
		}
	}

	if err := s.nodeManager.Heartbeat(
		heartbeat.CollectorId,
		time.Now(),
	); err != nil {
		return fmt.Errorf(
			"failed to update heartbeat for collector %q: %w",
			heartbeat.CollectorId,
			err,
		)
	}

	return nil
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
