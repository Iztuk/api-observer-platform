// Package watcher handles file watching
package watcher

import (
	"api-collector/internal/indexer"
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

func Watch(ctx context.Context, filePath string, lastBlock indexer.IndexBlock, tree *indexer.BPlusTree[int64, indexer.IndexBlock]) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file %q: %w", filePath, err)
	}
	defer file.Close()

	startOffset := lastBlock.EndOffset

	if _, err := file.Seek(startOffset, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek to offset %d: %w", startOffset, err)
	}

	reader := bufio.NewReader(file)

	var numLines uint32 = 0
	offset := startOffset
	blockSize := lastBlock.BlockSize

	blocks := make([]indexer.IndexBlock, 0)
	var block indexer.IndexBlock
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		default:
			lineStart := offset

			lineBytes, err := reader.ReadBytes('\n')

			if len(lineBytes) > 0 {
				numLines++

				offset += int64(len(lineBytes))

				if numLines == 1 {
					timestamp, err := indexer.ReadTimestamp(lineBytes)
					if err != nil {
						return err
					}

					block = indexer.IndexBlock{
						TimestampStart: timestamp,
						StartOffset:    lineStart,
						BlockSize:      blockSize,
					}
				}

				if numLines == blockSize {
					timestamp, err := indexer.ReadTimestamp(lineBytes)
					if err != nil {
						return err
					}

					block.TimestampEnd = timestamp
					block.EndOffset = offset

					tree.Insert(block.TimestampStart, block)
					blocks = append(blocks, block)

					numLines = 0
				}

			}

			if err != nil {
				if err == io.EOF {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(250 * time.Millisecond):
						if len(blocks) > 0 {
							if err := indexer.AppendIndexFile(fmt.Sprintf("%s.idx", filePath), blocks); err != nil {
								return err
							}

							blocks = make([]indexer.IndexBlock, 0)
						}
						continue
					}
				}

				return fmt.Errorf("failed reading file at offset %d: %w", offset, err)
			}
		}
	}
}
