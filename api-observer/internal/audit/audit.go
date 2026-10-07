// Package audit handles processing and storing jobs and findings.
package audit

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
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
	RequestID string
	Source    string
	Timestamp string
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
	Method        string
	URL           *url.URL
	Header        http.Header
	Body          string
	ContentLength int64

	Metadata Metadata
}

type ResponseJob struct {
	StatusCode    int
	Header        http.Header
	Body          string
	ContentLength int64

	Metadata Metadata
}

func (f Finding) Log(al, fl *log.Logger) {
	finding, err := json.Marshal(f)
	if err != nil {
		al.Printf("failed to marshal finding %v: \n%v", err)
	}

	fl.Println(string(finding))
}
