// Package watcher handles file watching.
package watcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"api-collector/internal/ingest"
	ingestv1 "api-collector/proto/ingest/v1"

	"github.com/nxadm/tail"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const modSecTimestampLayout = "Mon Jan _2 15:04:05 2006"

type ModSecLog struct {
	Transaction struct {
		TimeStamp string `json:"time_stamp"`
		UniqueID  string `json:"unique_id"`

		Request struct {
			Method  string        `json:"method"`
			URI     string        `json:"uri"`
			Headers ModSecHeaders `json:"headers"`
			Body    string        `json:"body"`
		} `json:"request"`

		Response struct {
			HTTPCode int32         `json:"http_code"`
			Headers  ModSecHeaders `json:"headers"`
			Body     string        `json:"body"`
		} `json:"response"`
	} `json:"transaction"`
}

type ModSecHeaders map[string][]string

func Watch(
	ctx context.Context,
	filePath string,
	ingestClient *ingest.Client,
	collectorID string,
) error {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf(
			"failed to get file info: %w",
			err,
		)
	}

	t, err := tail.TailFile(
		filePath,
		tail.Config{
			Location: &tail.SeekInfo{
				Offset: fileInfo.Size(),
			},

			ReOpen:        true,
			Follow:        true,
			CompleteLines: true,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"failed to start file tailing: %w",
			err,
		)
	}
	defer t.Cleanup()

	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-heartbeatTicker.C:
			if err := ingestClient.Heartbeat(ctx, collectorID); err != nil {
				return fmt.Errorf("failed to send heartbeat: %w", err)
			}

		case line, ok := <-t.Lines:
			if !ok {
				return nil
			}

			if line == nil {
				continue
			}

			if line.Err != nil {
				return fmt.Errorf(
					"failed while tailing %q: %w",
					filePath,
					line.Err,
				)
			}

			if line.Text == "" {
				continue
			}

			request, response, err := DecodeModSecLog(
				[]byte(line.Text),
				collectorID,
			)
			if err != nil {
				return fmt.Errorf(
					"failed to decode ModSecurity log: %w",
					err,
				)
			}

			if err := sendTransaction(
				ingestClient,
				request,
				response,
			); err != nil {
				log.Printf(
					"ingest connection lost: %v\n",
					err,
				)

				if err := ingestClient.ReconnectWithBackoff(
					ctx,
				); err != nil {
					return fmt.Errorf(
						"failed to reconnect to API Observer: %w",
						err,
					)
				}

				log.Println(
					"ingest connection restored; resuming watcher",
				)

				continue
			}
		}
	}
}

func sendTransaction(
	ingestClient *ingest.Client,
	request *ingestv1.Request,
	response *ingestv1.Response,
) error {
	if request != nil {
		record := &ingestv1.Record{
			Event: &ingestv1.Record_Request{
				Request: request,
			},
		}

		if _, err := ingestClient.Send(
			record,
		); err != nil {
			return fmt.Errorf(
				"failed to send request: %w",
				err,
			)
		}
	}

	if response != nil {
		record := &ingestv1.Record{
			Event: &ingestv1.Record_Response{
				Response: response,
			},
		}

		if _, err := ingestClient.Send(
			record,
		); err != nil {
			return fmt.Errorf(
				"failed to send response: %w",
				err,
			)
		}
	}

	return nil
}

func DecodeModSecLog(
	line []byte,
	collectorID string,
) (
	*ingestv1.Request,
	*ingestv1.Response,
	error,
) {
	var log ModSecLog

	if err := json.Unmarshal(
		line,
		&log,
	); err != nil {
		return nil, nil, fmt.Errorf(
			"failed to decode ModSecurity log: %w",
			err,
		)
	}

	transaction := log.Transaction

	timestamp, err := time.Parse(
		modSecTimestampLayout,
		transaction.TimeStamp,
	)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"failed to parse timestamp %q: %w",
			transaction.TimeStamp,
			err,
		)
	}

	requestMetadata := &ingestv1.Metadata{
		RequestId: transaction.UniqueID,
		Source:    collectorID,
		Timestamp: timestamppb.New(timestamp),
	}

	responseMetadata := &ingestv1.Metadata{
		RequestId: transaction.UniqueID,
		Source:    collectorID,
		Timestamp: timestamppb.New(timestamp),
	}

	request := &ingestv1.Request{
		Method: transaction.Request.Method,
		Url:    transaction.Request.URI,

		Headers: convertHeaders(
			transaction.Request.Headers,
		),

		Body: transaction.Request.Body,

		ContentLength: contentLength(
			transaction.Request.Headers,
			transaction.Request.Body,
		),

		Metadata: requestMetadata,
	}

	response := &ingestv1.Response{
		StatusCode: transaction.Response.HTTPCode,

		Headers: convertHeaders(
			transaction.Response.Headers,
		),

		Body: transaction.Response.Body,

		ContentLength: contentLength(
			transaction.Response.Headers,
			transaction.Response.Body,
		),

		Metadata: responseMetadata,
	}

	return request, response, nil
}

func convertHeaders(
	headers ModSecHeaders,
) map[string]*ingestv1.HeaderValues {
	result := make(
		map[string]*ingestv1.HeaderValues,
		len(headers),
	)

	for name, values := range headers {
		result[name] = &ingestv1.HeaderValues{
			Values: append(
				[]string(nil),
				values...,
			),
		}
	}

	return result
}

func contentLength(
	headers ModSecHeaders,
	body string,
) int64 {
	for name, values := range headers {
		if !strings.EqualFold(
			name,
			"Content-Length",
		) {
			continue
		}

		if len(values) == 0 {
			break
		}

		length, err := strconv.ParseInt(
			values[len(values)-1],
			10,
			64,
		)
		if err == nil {
			return length
		}
	}

	return int64(
		len(
			[]byte(body),
		),
	)
}

func (h *ModSecHeaders) UnmarshalJSON(
	data []byte,
) error {
	decoder := json.NewDecoder(
		bytes.NewReader(data),
	)

	token, err := decoder.Token()
	if err != nil {
		return err
	}

	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return fmt.Errorf(
			"expected headers object",
		)
	}

	headers := make(
		ModSecHeaders,
	)

	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}

		name, ok := token.(string)
		if !ok {
			return fmt.Errorf(
				"invalid header name",
			)
		}

		var value string

		if err := decoder.Decode(
			&value,
		); err != nil {
			return err
		}

		headers[name] = append(
			headers[name],
			value,
		)
	}

	if _, err := decoder.Token(); err != nil {
		return err
	}

	*h = headers

	return nil
}
