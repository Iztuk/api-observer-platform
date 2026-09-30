package query

import (
	queryv1 "api-collector/proto/query/v1"
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

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

func (h *ModSecHeaders) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))

	token, err := decoder.Token()
	if err != nil {
		return err
	}

	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return fmt.Errorf("expected headers object")
	}

	headers := make(ModSecHeaders)

	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}

		name, ok := token.(string)
		if !ok {
			return fmt.Errorf("invalid header name")
		}

		var value string

		if err := decoder.Decode(&value); err != nil {
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

func DecodeModSecLog(
	line []byte,
	collectorID string,
) (
	*queryv1.Request,
	*queryv1.Response,
	error,
) {
	var log ModSecLog

	if err := json.Unmarshal(line, &log); err != nil {
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

	metadata := &queryv1.Metadata{
		RequestId: transaction.UniqueID,
		Source:    collectorID,
		Timestamp: timestamppb.New(timestamp),
	}

	request := &queryv1.Request{
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

		Metadata: metadata,
	}

	response := &queryv1.Response{
		StatusCode: transaction.Response.HTTPCode,

		Headers: convertHeaders(
			transaction.Response.Headers,
		),

		Body: transaction.Response.Body,

		ContentLength: contentLength(
			transaction.Response.Headers,
			transaction.Response.Body,
		),

		Metadata: metadata,
	}

	return request, response, nil
}

func convertHeaders(
	headers ModSecHeaders,
) map[string]*queryv1.HeaderValues {
	result := make(
		map[string]*queryv1.HeaderValues,
		len(headers),
	)

	for name, values := range headers {
		result[name] = &queryv1.HeaderValues{
			Values: values,
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

	return int64(len([]byte(body)))
}
