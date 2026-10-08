// Package audit handles processing and storing jobs and findings.
package audit

import (
	"encoding/json"
	"fmt"
	"io"
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
) (int64, error) {
	findingsLogMu.Lock()
	defer findingsLogMu.Unlock()

	file, ok := fl.Writer().(*os.File)
	if !ok {
		err := fmt.Errorf(
			"findings logger is not writing directly to a file",
		)

		al.Println(err)

		return 0, err
	}

	finding, err := json.Marshal(f)
	if err != nil {
		err = fmt.Errorf(
			"failed to marshal finding: %w",
			err,
		)

		al.Println(err)

		return 0, err
	}

	offset, err := file.Seek(
		0,
		io.SeekEnd,
	)
	if err != nil {
		err = fmt.Errorf(
			"failed to get findings log offset: %w",
			err,
		)

		al.Println(err)

		return 0, err
	}

	fl.Println(
		string(finding),
	)

	return offset, nil
}

func (f Finding) Index(al *log.Logger, offset int64) error {
	var nodeID string

	if f.Metadata.Request != nil {
		nodeID = f.Metadata.Request.Metadata.Source
	} else if f.Metadata.Response != nil {
		nodeID = f.Metadata.Response.Metadata.Source
	} else {
		e := fmt.Errorf("missing source ID: %v", f)
		al.Println(e)
		return e
	}

	var data []byte

	err := os.WriteFile(fmt.Sprintf("%s.idx", nodeID), data, 0644)
	if err != nil {
		e := fmt.Errorf("failed to write finding index: %v", err)
		al.Println(e)
		return e
	}

	return nil
}
