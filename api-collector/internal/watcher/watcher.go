// Package watcher handles file watching
package watcher

import (
	"api-collector/internal/indexer"
	"api-collector/internal/ingest"
	"api-collector/internal/query"
	ingestv1 "api-collector/proto/ingest/v1"
	queryv1 "api-collector/proto/query/v1"

	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

func Watch(
	ctx context.Context,
	filePath string,
	lastBlock indexer.IndexBlock,
	tree *indexer.BPlusTree[int64, indexer.IndexBlock],
	ingestClient *ingest.Client,
	collectorID string,
) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf(
			"failed to open file %q: %w",
			filePath,
			err,
		)
	}
	defer file.Close()

	startOffset := lastBlock.EndOffset

	if _, err := file.Seek(
		startOffset,
		io.SeekStart,
	); err != nil {
		return fmt.Errorf(
			"failed to seek to offset %d: %w",
			startOffset,
			err,
		)
	}

	reader := bufio.NewReader(file)

	var numLines uint32

	offset := startOffset
	blockSize := lastBlock.BlockSize

	blocks := make(
		[]indexer.IndexBlock,
		0,
	)

	var block indexer.IndexBlock

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		default:
			lineStart := offset

			lineBytes, err :=
				reader.ReadBytes('\n')

			if len(lineBytes) > 0 {
				if lineBytes[len(lineBytes)-1] != '\n' {
					if _, seekErr := file.Seek(
						lineStart,
						io.SeekStart,
					); seekErr != nil {
						return seekErr
					}

					reader.Reset(file)

					time.Sleep(
						250 * time.Millisecond,
					)

					continue
				}

				if ingestClient.Connected() {
					request, response, decodeErr :=
						query.DecodeModSecLog(
							lineBytes,
							collectorID,
						)
					if decodeErr != nil {
						fmt.Printf(
							"failed to decode ModSecurity log at offset %d: %v\n",
							lineStart,
							decodeErr,
						)
					} else {
						requestRecord :=
							&ingestv1.Record{
								Event: &ingestv1.Record_Request{
									Request: queryRequestToIngest(
										request,
									),
								},
							}

						if _, sendErr :=
							ingestClient.Send(
								requestRecord,
							); sendErr != nil {

							fmt.Printf(
								"failed to ingest request at offset %d: %v\n",
								lineStart,
								sendErr,
							)
						}

						responseRecord :=
							&ingestv1.Record{
								Event: &ingestv1.Record_Response{
									Response: queryResponseToIngest(
										response,
									),
								},
							}

						if _, sendErr :=
							ingestClient.Send(
								responseRecord,
							); sendErr != nil {

							fmt.Printf(
								"failed to ingest response at offset %d: %v\n",
								lineStart,
								sendErr,
							)
						}
					}
				}

				numLines++

				offset += int64(
					len(lineBytes),
				)

				if numLines == 1 {
					timestamp, err :=
						indexer.ReadTimestamp(
							lineBytes,
						)
					if err != nil {
						return err
					}

					block =
						indexer.IndexBlock{
							TimestampStart: timestamp,

							StartOffset: lineStart,

							BlockSize: blockSize,
						}
				}

				if numLines == blockSize {
					timestamp, err :=
						indexer.ReadTimestamp(
							lineBytes,
						)
					if err != nil {
						return err
					}

					block.TimestampEnd =
						timestamp

					block.EndOffset =
						offset

					tree.Insert(
						block.TimestampStart,
						block,
					)

					blocks = append(
						blocks,
						block,
					)

					numLines = 0
				}
			}

			if err != nil {
				if err == io.EOF {
					select {
					case <-ctx.Done():
						return ctx.Err()

					case <-time.After(
						250 * time.Millisecond,
					):
						if len(blocks) > 0 {
							if err :=
								indexer.AppendIndexFile(
									fmt.Sprintf(
										"%s.idx",
										filePath,
									),
									blocks,
								); err != nil {

								return err
							}

							blocks =
								make(
									[]indexer.IndexBlock,
									0,
								)
						}

						continue
					}
				}

				return fmt.Errorf(
					"failed reading file at offset %d: %w",
					offset,
					err,
				)
			}
		}
	}
}

func queryRequestToIngest(
	request *queryv1.Request,
) *ingestv1.Request {
	if request == nil {
		return nil
	}

	headers := make(
		map[string]*ingestv1.HeaderValues,
		len(request.Headers),
	)

	for key, values := range request.Headers {

		headers[key] =
			&ingestv1.HeaderValues{
				Values: append(
					[]string(nil),
					values.Values...,
				),
			}
	}

	var metadata *ingestv1.Metadata

	if request.Metadata != nil {
		metadata =
			&ingestv1.Metadata{
				RequestId: request.Metadata.RequestId,

				Source: request.Metadata.Source,

				Timestamp: request.Metadata.Timestamp,
			}
	}

	return &ingestv1.Request{
		Method: request.Method,

		Url: request.Url,

		Headers: headers,

		Body: request.Body,

		ContentLength: request.ContentLength,

		Metadata: metadata,
	}
}

func queryResponseToIngest(
	response *queryv1.Response,
) *ingestv1.Response {
	if response == nil {
		return nil
	}

	headers := make(
		map[string]*ingestv1.HeaderValues,
		len(response.Headers),
	)

	for key, values := range response.Headers {

		headers[key] =
			&ingestv1.HeaderValues{
				Values: append(
					[]string(nil),
					values.Values...,
				),
			}
	}

	var metadata *ingestv1.Metadata

	if response.Metadata != nil {
		metadata =
			&ingestv1.Metadata{
				RequestId: response.Metadata.RequestId,

				Source: response.Metadata.Source,

				Timestamp: response.Metadata.Timestamp,
			}
	}

	return &ingestv1.Response{
		StatusCode: response.StatusCode,

		Headers: headers,

		Body: response.Body,

		ContentLength: response.ContentLength,

		Metadata: metadata,
	}
}
