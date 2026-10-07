package ingest

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"api-observer/internal/audit"
	ingestv1 "api-observer/proto/ingest/v1"
)

func toAuditJob(
	pb *ingestv1.Record,
) (audit.Job, error) {
	if pb == nil {
		return audit.Job{},
			fmt.Errorf("record is nil")
	}

	switch event := pb.Event.(type) {
	case *ingestv1.Record_Request:
		if event.Request == nil {
			return audit.Job{},
				fmt.Errorf("missing request")
		}

		req := event.Request

		if req.Metadata == nil {
			return audit.Job{},
				fmt.Errorf("missing request metadata")
		}

		if req.Metadata.RequestId == "" {
			return audit.Job{},
				fmt.Errorf("missing request ID")
		}

		if req.Url == "" {
			return audit.Job{},
				fmt.Errorf("missing request URL")
		}

		u, err := url.Parse(
			req.Url,
		)
		if err != nil {
			return audit.Job{},
				fmt.Errorf(
					"invalid request URL: %w",
					err,
				)
		}

		job := audit.Job{
			Request: &audit.RequestJob{
				Method: req.Method,
				URL:    u,

				Header: toHTTPHeaders(
					req.Headers,
				),

				Body: req.Body,

				ContentLength: req.ContentLength,

				Metadata: audit.Metadata{
					RequestID: req.Metadata.RequestId,
					Source:    req.Metadata.Source,
				},
			},
		}

		if req.Metadata.Timestamp != nil {
			job.Request.Metadata.Timestamp =
				req.Metadata.Timestamp.
					AsTime().
					Format(time.RFC3339Nano)
		}

		return job, nil

	case *ingestv1.Record_Response:
		if event.Response == nil {
			return audit.Job{},
				fmt.Errorf("missing response")
		}

		resp := event.Response

		if resp.Metadata == nil {
			return audit.Job{},
				fmt.Errorf("missing response metadata")
		}

		if resp.Metadata.RequestId == "" {
			return audit.Job{},
				fmt.Errorf("missing request ID")
		}

		job := audit.Job{
			Response: &audit.ResponseJob{
				StatusCode: int(
					resp.StatusCode,
				),

				Header: toHTTPHeaders(
					resp.Headers,
				),

				Body: resp.Body,

				ContentLength: resp.ContentLength,

				Metadata: audit.Metadata{
					RequestID: resp.Metadata.RequestId,
					Source:    resp.Metadata.Source,
				},
			},
		}

		if resp.Metadata.Timestamp != nil {
			job.Response.Metadata.Timestamp =
				resp.Metadata.Timestamp.
					AsTime().
					Format(time.RFC3339Nano)
		}

		return job, nil

	default:
		return audit.Job{},
			fmt.Errorf(
				"record does not contain a request or response",
			)
	}
}

func toHTTPHeaders(
	headers map[string]*ingestv1.HeaderValues,
) http.Header {
	result := make(
		http.Header,
	)

	for name, values := range headers {
		if values == nil {
			continue
		}

		result[http.CanonicalHeaderKey(name)] = append(
			[]string(nil),
			values.Values...,
		)
	}

	return result
}
