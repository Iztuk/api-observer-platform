// Package ingest handles log ingestion to API Observer for auditing
package ingest

import (
	"context"
	"fmt"
	"sync"

	ingestv1 "api-collector/proto/ingest/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
	c.mu.Lock()
	defer c.mu.Unlock()

	if address == "" {
		return fmt.Errorf(
			"ingest address is empty",
		)
	}

	if c.conn != nil &&
		c.stream != nil &&
		c.address == address {

		return nil
	}

	if c.conn != nil {
		_ = c.conn.Close()

		c.conn = nil
		c.client = nil
		c.stream = nil
		c.address = ""
	}

	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(
			insecure.NewCredentials(),
		),
	)
	if err != nil {
		return fmt.Errorf(
			"failed to create ingest connection to %s: %w",
			address,
			err,
		)
	}

	client :=
		ingestv1.NewIngestServiceClient(
			conn,
		)

	stream, err :=
		client.Ingest(ctx)
	if err != nil {
		_ = conn.Close()

		return fmt.Errorf(
			"failed to open ingest stream to %s: %w",
			address,
			err,
		)
	}

	c.conn = conn
	c.client = client
	c.stream = stream
	c.address = address

	return nil
}

func (c *Client) Send(
	record *ingestv1.Record,
) (*ingestv1.IngestAck, error) {
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
		return nil, fmt.Errorf(
			"failed to send ingest record: %w",
			err,
		)
	}

	ack, err := c.stream.Recv()
	if err != nil {
		return nil, fmt.Errorf(
			"failed to receive ingest acknowledgement: %w",
			err,
		)
	}

	return ack, nil
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
		return nil
	}

	err := c.conn.Close()

	c.conn = nil
	c.client = nil
	c.stream = nil
	c.address = ""

	return err
}
