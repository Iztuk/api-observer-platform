// Package query handles querying operations from API Observer.
package query

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"api-collector/internal/indexer"
	"api-collector/internal/ingest"
	queryv1 "api-collector/proto/query/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	queryv1.UnimplementedLogServiceServer

	ctx context.Context

	filePath string

	tree *indexer.BPlusTree[
		int64,
		indexer.IndexBlock,
	]

	collectorID string

	ingestClient *ingest.Client
}

func NewServer(
	ctx context.Context,
	filePath string,
	tree *indexer.BPlusTree[
		int64,
		indexer.IndexBlock,
	],
	collectorID string,
	ingestClient *ingest.Client,
) *Server {
	return &Server{
		ctx:          ctx,
		filePath:     filePath,
		tree:         tree,
		collectorID:  collectorID,
		ingestClient: ingestClient,
	}
}

func (s *Server) Info(
	ctx context.Context,
	req *queryv1.ObserverInfo,
) (*queryv1.InfoReply, error) {
	if req == nil {
		return nil,
			status.Error(
				codes.InvalidArgument,
				"info request is nil",
			)
	}

	if req.IngestAddress == "" {
		return nil,
			status.Error(
				codes.InvalidArgument,
				"ingest address is required",
			)
	}

	if err := s.ingestClient.Connect(
		s.ctx,
		req.IngestAddress,
	); err != nil {
		return nil,
			status.Errorf(
				codes.Unavailable,
				"failed to connect to Observer ingest service: %v",
				err,
			)
	}

	fmt.Printf(
		"Connected to API Observer ingest service at %s\n",
		req.IngestAddress,
	)

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

	if params.Cursor > 0 &&
		params.Cursor < endOffset {

		endOffset = params.Cursor
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

	offset := endOffset

	for offset > startOffset {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()

		default:
		}

		lineBytes, lineStart, err := readPreviousLine(
			file,
			startOffset,
			offset,
		)
		if err != nil {
			if err == io.EOF {
				break
			}

			return fmt.Errorf(
				"failed reading log file backwards from offset %d: %w",
				offset,
				err,
			)
		}

		offset = lineStart

		if len(lineBytes) == 0 {
			continue
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

		if logTime.After(endTime) {
			continue
		}

		if logTime.Before(startTime) {
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

		nextCursor := lineStart

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

	return nil
}

func readPreviousLine(
	file *os.File,
	startOffset int64,
	endOffset int64,
) (
	[]byte,
	int64,
	error,
) {
	if endOffset <= startOffset {
		return nil,
			startOffset,
			io.EOF
	}

	cursor := endOffset

	lastByte := make(
		[]byte,
		1,
	)

	if _, err := file.ReadAt(
		lastByte,
		cursor-1,
	); err != nil {
		return nil,
			startOffset,
			err
	}

	if lastByte[0] == '\n' {
		cursor--
	}

	lineEnd := cursor

	for cursor > startOffset {
		cursor--

		b := make(
			[]byte,
			1,
		)

		if _, err := file.ReadAt(
			b,
			cursor,
		); err != nil {
			return nil,
				startOffset,
				err
		}

		if b[0] == '\n' {
			cursor++
			break
		}
	}

	if cursor < startOffset {
		cursor = startOffset
	}

	lineLength := lineEnd - cursor

	if lineLength <= 0 {
		return nil,
			cursor,
			nil
	}

	lineBytes := make(
		[]byte,
		lineLength,
	)

	if _, err := file.ReadAt(
		lineBytes,
		cursor,
	); err != nil {
		return nil,
			cursor,
			err
	}

	return lineBytes,
		cursor,
		nil
}
