// Package audit handles processing and storing jobs and findings.
package audit

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
)

type Finding struct {
	Title    string   `json:"title"`
	Message  string   `json:"message"`
	Severity string   `json:"severity"`
	Tags     []string `json:"tags"`

	Metadata    FindingMetadata `json:"metadata"`
	ProcessedAt string          `json:"processed_at"`
}

type FindingMetadata struct {
	Request  *RequestJob  `json:"request,omitempty"`
	Response *ResponseJob `json:"response,omitempty"`
}

type Metadata struct {
	RequestID string `json:"request_id"`
	Source    string `json:"source"`
	Timestamp int64  `json:"timestamp"`
}

type Job struct {
	Type JobType

	Request  *RequestJob
	Response *ResponseJob
}

type JobType string

const (
	JobTypeRequest  JobType = "request"
	JobTypeResponse JobType = "response"
)

type RequestJob struct {
	Method        string      `json:"method"`
	URL           *url.URL    `json:"url"`
	Headers       http.Header `json:"headers"`
	Body          string      `json:"body"`
	ContentLength int64       `json:"content_length"`

	Metadata Metadata `json:"metadata"`
}

type ResponseJob struct {
	StatusCode    int         `json:"status_code"`
	Headers       http.Header `json:"headers"`
	Body          string      `json:"body"`
	ContentLength int64       `json:"content_length"`

	Metadata Metadata `json:"metadata"`
}

var findingsLogMu sync.Mutex

func (f Finding) Log(
	al *log.Logger,
	fl *log.Logger,
) error {
	findingsLogMu.Lock()
	defer findingsLogMu.Unlock()

	if _, ok := fl.Writer().(*os.File); !ok {
		err := fmt.Errorf(
			"findings logger is not writing directly to a file",
		)

		al.Println(err)

		return err
	}

	finding, err := json.Marshal(
		f,
	)
	if err != nil {
		err = fmt.Errorf(
			"failed to marshal finding: %w",
			err,
		)

		al.Println(err)

		return err
	}

	fl.Println(
		string(finding),
	)

	return nil
}
