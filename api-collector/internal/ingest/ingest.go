// Package ingest handles log ingestion to API Observer for auditing.
package ingest

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	ingestv1 "api-collector/proto/ingest/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	initialReconnectDelay = time.Second
	maxReconnectDelay     = 30 * time.Second
)

type Client struct {
	mu sync.Mutex

	conn   *grpc.ClientConn
	client ingestv1.IngestServiceClient
	stream ingestv1.IngestService_IngestClient

	address string
}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) Connect(
	ctx context.Context,
	address string,
) error {
	if address == "" {
		return fmt.Errorf(
			"ingest address is empty",
		)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil &&
		c.stream != nil &&
		c.address == address {

		return nil
	}

	c.address = address

	return c.connectLocked(ctx)
}

func (c *Client) Reconnect(
	ctx context.Context,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.address == "" {
		return fmt.Errorf(
			"ingest address is not configured",
		)
	}

	return c.connectLocked(ctx)
}

func (c *Client) connectLocked(
	ctx context.Context,
) error {
	if c.conn != nil {
		_ = c.conn.Close()
	}

	c.conn = nil
	c.client = nil
	c.stream = nil

	conn, err := grpc.NewClient(
		c.address,
		grpc.WithTransportCredentials(
			insecure.NewCredentials(),
		),
	)
	if err != nil {
		return fmt.Errorf(
			"failed to create ingest connection to %s: %w",
			c.address,
			err,
		)
	}

	client := ingestv1.NewIngestServiceClient(
		conn,
	)

	stream, err := client.Ingest(ctx)
	if err != nil {
		_ = conn.Close()

		return fmt.Errorf(
			"failed to open ingest stream to %s: %w",
			c.address,
			err,
		)
	}

	c.conn = conn
	c.client = client
	c.stream = stream

	return nil
}

func (c *Client) Send(
	record *ingestv1.Record,
) (*ingestv1.IngestAck, error) {
	if record == nil {
		return nil, fmt.Errorf(
			"ingest record is nil",
		)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stream == nil {
		return nil, fmt.Errorf(
			"ingest client is not connected",
		)
	}

	if err := c.stream.Send(
		record,
	); err != nil {
		c.stream = nil

		return nil, fmt.Errorf(
			"failed to send ingest record: %w",
			err,
		)
	}

	ack, err := c.stream.Recv()
	if err != nil {
		c.stream = nil

		return nil, fmt.Errorf(
			"failed to receive ingest acknowledgement: %w",
			err,
		)
	}

	return ack, nil
}

func (c *Client) Heartbeat(
	ctx context.Context,
	collectorID,
	queryAddr string,
) error {
	record := newHeartbeatRecord(collectorID, queryAddr)

	if _, err := c.Send(record); err == nil {
		return nil
	}

	if err := c.ReconnectWithBackoff(
		ctx,
	); err != nil {
		return fmt.Errorf(
			"failed to reconnect ingest client: %w",
			err,
		)
	}

	// Heartbeats are safe to retry after reconnecting.
	record = newHeartbeatRecord(collectorID, queryAddr)

	if _, err := c.Send(record); err != nil {
		return fmt.Errorf(
			"failed to send heartbeat after reconnect: %w",
			err,
		)
	}

	return nil
}

func newHeartbeatRecord(
	collectorID,
	queryAddr string,
) *ingestv1.Record {
	return &ingestv1.Record{
		Event: &ingestv1.Record_Heartbeat{
			Heartbeat: &ingestv1.Heartbeat{
				CollectorId: collectorID,
				QueryAddr:   queryAddr,
				Timestamp:   timestamppb.Now(),
			},
		},
	}
}

func (c *Client) ReconnectWithBackoff(
	ctx context.Context,
) error {
	delay := initialReconnectDelay

	for {
		if err := c.Reconnect(
			ctx,
		); err == nil {

			log.Printf(
				"reconnected to API Observer at %s\n",
				c.Address(),
			)

			return nil
		} else {
			jitter := time.Duration(float64(delay) * (rand.Float64()*0.4 - 0.2))
			wait := delay + jitter

			log.Printf(
				"failed to reconnect to API Observer: %v; retrying in %s\n",
				err,
				wait,
			)

			timer := time.NewTimer(
				wait,
			)

			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}

				return ctx.Err()

			case <-timer.C:
			}
		}

		delay *= 2

		if delay > maxReconnectDelay {
			delay = maxReconnectDelay
		}
	}
}

func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.stream != nil
}

func (c *Client) Address() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.address
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		c.client = nil
		c.stream = nil
		c.address = ""

		return nil
	}

	err := c.conn.Close()

	c.conn = nil
	c.client = nil
	c.stream = nil
	c.address = ""

	return err
}
