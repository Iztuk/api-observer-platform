// Package query handles querying operations from API Observer.
package query

import (
	"api-collector/internal/indexer"
	queryv1 "api-collector/proto/query/v1"
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"
)

type Server struct {
	queryv1.UnimplementedLogServiceServer

	filePath    string
	tree        *indexer.BPlusTree[int64, indexer.IndexBlock]
	collectorID string
}

func NewServer(
	filePath string,
	tree *indexer.BPlusTree[int64, indexer.IndexBlock],
	collectorID string,
) *Server {
	return &Server{
		filePath:    filePath,
		tree:        tree,
		collectorID: collectorID,
	}
}

func (s *Server) Info(
	ctx context.Context,
	req *emptypb.Empty,
) (*queryv1.InfoReply, error) {
	return &queryv1.InfoReply{
		CollectorId: s.collectorID,
	}, nil
}

func (s *Server) Query(
	params *queryv1.LogParams,
	stream queryv1.LogService_QueryServer,
) error {
	if params == nil {
		return fmt.Errorf("missing query parameters")
	}

	if params.StartDate == nil {
		return fmt.Errorf("missing start date")
	}

	if params.EndDate == nil {
		return fmt.Errorf("missing end date")
	}

	if err := params.StartDate.CheckValid(); err != nil {
		return fmt.Errorf(
			"invalid start date: %w",
			err,
		)
	}

	if err := params.EndDate.CheckValid(); err != nil {
		return fmt.Errorf(
			"invalid end date: %w",
			err,
		)
	}

	if s.tree == nil {
		return fmt.Errorf(
			"index tree is not initialized",
		)
	}

	startTime := params.StartDate.AsTime()
	endTime := params.EndDate.AsTime()

	if endTime.Before(startTime) {
		return fmt.Errorf(
			"end date %s is before start date %s",
			endTime,
			startTime,
		)
	}

	startTimestamp := startTime.UnixNano()
	endTimestamp := endTime.UnixNano()

	startBlock, endBlock, ok := s.findBlocks(
		startTimestamp,
		endTimestamp,
	)
	if !ok {
		return nil
	}

	startOffset := startBlock.StartOffset
	endOffset := endBlock.EndOffset

	/*
		Cursor is treated as an absolute byte offset.

		This only works safely if cursors always point to the
		beginning of a log record.
	*/
	if params.Cursor > startOffset {
		startOffset = params.Cursor
	}

	if startOffset >= endOffset {
		return nil
	}

	return s.streamLogs(
		stream,
		startOffset,
		endOffset,
		startTime,
		endTime,
	)
}

func (s *Server) findBlocks(
	startTimestamp int64,
	endTimestamp int64,
) (
	indexer.IndexBlock,
	indexer.IndexBlock,
	bool,
) {
	var startBlock indexer.IndexBlock
	var endBlock indexer.IndexBlock

	_, floorStart, hasStart := s.tree.FindFloor(
		startTimestamp,
	)

	_, floorEnd, hasEnd := s.tree.FindFloor(
		endTimestamp,
	)

	entries := s.tree.Range(
		startTimestamp,
		endTimestamp,
	)

	if hasStart {
		startBlock = floorStart
	} else if len(entries) > 0 {
		startBlock = entries[0].Value
	} else {
		return indexer.IndexBlock{},
			indexer.IndexBlock{},
			false
	}

	if hasEnd {
		endBlock = floorEnd
	} else if len(entries) > 0 {
		endBlock = entries[len(entries)-1].Value
	} else {
		return indexer.IndexBlock{},
			indexer.IndexBlock{},
			false
	}

	return startBlock,
		endBlock,
		true
}

func (s *Server) streamLogs(
	stream queryv1.LogService_QueryServer,
	startOffset int64,
	endOffset int64,
	startTime time.Time,
	endTime time.Time,
) error {
	file, err := os.Open(s.filePath)
	if err != nil {
		return fmt.Errorf(
			"failed to open log file %q: %w",
			s.filePath,
			err,
		)
	}
	defer file.Close()

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

	offset := startOffset

	for offset < endOffset {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()

		default:
		}

		lineStart := offset

		lineBytes, readErr := reader.ReadBytes('\n')

		if len(lineBytes) > 0 {
			offset += int64(len(lineBytes))

			if lineStart >= endOffset {
				break
			}

			timestamp, err := indexer.ReadTimestamp(
				lineBytes,
			)
			if err != nil {
				return fmt.Errorf(
					"failed to read timestamp at offset %d: %w",
					lineStart,
					err,
				)
			}

			logTime := time.Unix(
				0,
				timestamp,
			)

			if logTime.Before(startTime) {
				continue
			}

			if logTime.After(endTime) {
				break
			}

			request, response, err := DecodeModSecLog(
				lineBytes,
				s.collectorID,
			)
			if err != nil {
				return fmt.Errorf(
					"failed to decode ModSecurity log at offset %d: %w",
					lineStart,
					err,
				)
			}

			nextCursor := offset

			requestLog := &queryv1.Log{
				Cursor: nextCursor,

				Event: &queryv1.Log_Request{
					Request: request,
				},
			}

			responseLog := &queryv1.Log{
				Cursor: nextCursor,

				Event: &queryv1.Log_Response{
					Response: response,
				},
			}

			if err := stream.Send(
				requestLog,
			); err != nil {
				return fmt.Errorf(
					"failed to stream request at offset %d: %w",
					lineStart,
					err,
				)
			}

			if err := stream.Send(
				responseLog,
			); err != nil {
				return fmt.Errorf(
					"failed to stream response at offset %d: %w",
					lineStart,
					err,
				)
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}

			return fmt.Errorf(
				"failed reading log file at offset %d: %w",
				offset,
				readErr,
			)
		}
	}

	return nil
}
