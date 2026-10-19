package ingest

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"api-observer/internal/audit"
	ingestv1 "api-observer/proto/ingest/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestIngestBidirectional(t *testing.T) {
	// A queue of capacity 1 lets us test backpressure.
	queue := audit.NewQueue(1)
	defer queue.Close()

	// Create an in-memory network listener.
	listener := bufconn.Listen(1024 * 1024)

	// Start the actual gRPC server.
	grpcServer := grpc.NewServer()

	ingestv1.RegisterIngestServiceServer(
		grpcServer,
		NewServer(queue),
	)

	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			t.Logf("gRPC server stopped: %v", err)
		}
	}()

	defer grpcServer.Stop()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(
			func(ctx context.Context, address string) (net.Conn, error) {
				return listener.DialContext(ctx)
			},
		),
		grpc.WithTransportCredentials(
			insecure.NewCredentials(),
		),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer conn.Close()

	client := ingestv1.NewIngestServiceClient(conn)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	// Establish the bidirectional stream.
	stream, err := client.Ingest(ctx)
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}

	// Helper to construct valid test jobs.
	makeJob := func(id string) *ingestv1.Job {
		return &ingestv1.Job{
			Id:   id,
			Type: ingestv1.JobType_JOB_TYPE_REQUEST,
			Request: &ingestv1.Request{
				Method: "GET",
				Url:    "http://localhost:8080/admin",
				Metadata: &ingestv1.Metadata{
					RequestId: "request-123",
					Source:    "test-collector",
				},
			},
		}
	}

	// First job: should be accepted.
	if err := stream.Send(makeJob("job-001")); err != nil {
		t.Fatalf("failed to send first job: %v", err)
	}

	ack, err := stream.Recv()
	if err != nil {
		t.Fatalf("failed to receive first ACK: %v", err)
	}

	if ack.JobId != "job-001" {
		t.Fatalf("unexpected job ID: %s", ack.JobId)
	}

	if ack.Status != ingestv1.IngestStatus_INGEST_STATUS_ACCEPTED {
		t.Fatalf("expected ACCEPTED, got %s", ack.Status)
	}

	t.Logf("First ACK: %+v", ack)

	// Second job: queue is full, so it should be rejected.
	if err := stream.Send(makeJob("job-002")); err != nil {
		t.Fatalf("failed to send second job: %v", err)
	}

	ack, err = stream.Recv()
	if err != nil {
		t.Fatalf("failed to receive second ACK: %v", err)
	}

	if ack.JobId != "job-002" {
		t.Fatalf("unexpected job ID: %s", ack.JobId)
	}

	if ack.Status != ingestv1.IngestStatus_INGEST_STATUS_REJECTED {
		t.Fatalf("expected REJECTED, got %s", ack.Status)
	}

	if !ack.Retryable {
		t.Fatal("expected queue-full rejection to be retryable")
	}

	t.Logf("Second ACK: %+v", ack)

	// Finish sending jobs.
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("failed to close send stream: %v", err)
	}

	// Server should finish after the client closes its sending side.
	_, err = stream.Recv()
	if err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}
