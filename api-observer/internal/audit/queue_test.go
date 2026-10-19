package audit

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/url"
	"testing"
)

func TestQueue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	appLogger := log.New(io.Discard, "", 0)
	findingsLogger := log.New(io.Discard, "", 0)

	rs := NewRuleSet()

	q := NewQueue(10)

	reqURL, err := url.Parse("http://localhost/users")
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}

	jobs := []Job{
		{
			Type: JobTypeRequest,
			Request: &RequestJob{
				Method:        http.MethodGet,
				URL:           reqURL,
				Header:        http.Header{},
				Body:          "",
				ContentLength: 0,
				Metadata: Metadata{
					RequestID: "request-1",
					Source:    "test",
				},
			},
		},
		{
			Type: JobTypeResponse,
			Request: &RequestJob{
				Method:        http.MethodGet,
				URL:           reqURL,
				Header:        http.Header{},
				Body:          "",
				ContentLength: 0,
				Metadata: Metadata{
					RequestID: "request-1",
					Source:    "test",
				},
			},
			Response: &ResponseJob{
				StatusCode:    http.StatusOK,
				Header:        http.Header{},
				Body:          `{"status":"ok"}`,
				ContentLength: 15,
				Metadata: Metadata{
					RequestID: "request-1",
					Source:    "test",
				},
			},
		},
	}

	wg := q.StartWorkers(
		ctx,
		rs,
		1,
		appLogger,
		findingsLogger,
	)

	for i, job := range jobs {
		if ok := q.TryEnqueue(job); !ok {
			t.Fatalf("failed to enqueue job %d", i)
		}
	}

	q.Close()
	wg.Wait()
}
