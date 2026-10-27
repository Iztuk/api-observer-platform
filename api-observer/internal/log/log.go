// Package log handles log streaming.
package log

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"api-observer/internal/nodes"
	"api-observer/internal/query"
	ingestv1 "api-observer/proto/ingest/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	collectorQueryPort = "24900"
	streamBatchSize    = 1000
)

type StreamParams struct {
	Query string

	Start time.Time
	End   time.Time

	StartString string
	EndString   string
}

type StreamLogBatch struct {
	NodeID   string
	NodeName string

	Logs query.LogItems

	Done bool
	Err  error
}

func StreamCollectorLogs(
	ctx context.Context,
	node *nodes.Node,
	params StreamParams,
	output chan<- StreamLogBatch,
) {
	if node == nil {
		sendCollectorBatch(
			ctx,
			output,
			StreamLogBatch{
				Err: fmt.Errorf(
					"node is nil",
				),
			},
		)

		return
	}

	queryAddress, err :=
		collectorQueryAddress(
			node,
		)

	if err != nil {
		sendCollectorBatch(
			ctx,
			output,
			StreamLogBatch{
				NodeID:   node.ID,
				NodeName: node.Name,
				Err:      err,
			},
		)

		return
	}

	conn, err :=
		grpc.NewClient(
			queryAddress,
			grpc.WithTransportCredentials(
				insecure.NewCredentials(),
			),
		)

	if err != nil {
		sendCollectorBatch(
			ctx,
			output,
			StreamLogBatch{
				NodeID:   node.ID,
				NodeName: node.Name,

				Err: fmt.Errorf(
					"failed to connect to collector: %w",
					err,
				),
			},
		)

		return
	}

	defer conn.Close()

	client :=
		ingestv1.NewIngestServiceClient(
			conn,
		)

	stream, err :=
		client.Logs(
			ctx,
			&ingestv1.LogRequest{
				StartDate: timestamppb.New(
					params.Start,
				),

				EndDate: timestamppb.New(
					params.End,
				),
			},
		)

	if err != nil {
		sendCollectorBatch(
			ctx,
			output,
			StreamLogBatch{
				NodeID:   node.ID,
				NodeName: node.Name,

				Err: fmt.Errorf(
					"failed to start log stream: %w",
					err,
				),
			},
		)

		return
	}

	logs := make(
		query.LogItems,
		0,
		streamBatchSize,
	)

	flush := func() bool {
		if len(logs) == 0 {
			return true
		}

		batch := make(
			query.LogItems,
			len(logs),
		)

		copy(
			batch,
			logs,
		)

		if !sendCollectorBatch(
			ctx,
			output,
			StreamLogBatch{
				NodeID:   node.ID,
				NodeName: node.Name,
				Logs:     batch,
			},
		) {
			return false
		}

		logs = logs[:0]

		return true
	}

	for {
		transaction, err :=
			stream.Recv()

		if errors.Is(
			err,
			io.EOF,
		) {
			if !flush() {
				return
			}

			sendCollectorBatch(
				ctx,
				output,
				StreamLogBatch{
					NodeID:   node.ID,
					NodeName: node.Name,
					Done:     true,
				},
			)

			return
		}

		if err != nil {
			if ctx.Err() != nil {
				return
			}

			sendCollectorBatch(
				ctx,
				output,
				StreamLogBatch{
					NodeID:   node.ID,
					NodeName: node.Name,

					Err: fmt.Errorf(
						"log stream failed: %w",
						err,
					),
				},
			)

			return
		}

		if transaction == nil {
			continue
		}

		if transaction.Request != nil {
			logs = append(
				logs,
				query.LogItem{
					Log: &ingestv1.Record{
						Event: &ingestv1.Record_Request{
							Request: transaction.Request,
						},
					},

					NodeName: node.Name,
				},
			)
		}

		if transaction.Response != nil {
			logs = append(
				logs,
				query.LogItem{
					Log: &ingestv1.Record{
						Event: &ingestv1.Record_Response{
							Response: transaction.Response,
						},
					},

					NodeName: node.Name,
				},
			)
		}

		if len(logs) >=
			streamBatchSize {

			if !flush() {
				return
			}
		}
	}
}

func sendCollectorBatch(
	ctx context.Context,
	output chan<- StreamLogBatch,
	batch StreamLogBatch,
) bool {
	select {
	case output <- batch:
		return true

	case <-ctx.Done():
		return false
	}
}

func collectorQueryAddress(
	node *nodes.Node,
) (string, error) {
	if node == nil {
		return "",
			fmt.Errorf(
				"node is nil",
			)
	}

	if node.Addr == "" {
		return "",
			fmt.Errorf(
				"node %q does not have an address",
				node.ID,
			)
	}

	host, _, err :=
		net.SplitHostPort(
			node.Addr,
		)

	if err != nil {
		return "",
			fmt.Errorf(
				"invalid node address %q: %w",
				node.Addr,
				err,
			)
	}

	return net.JoinHostPort(
		host,
		collectorQueryPort,
	), nil
}
