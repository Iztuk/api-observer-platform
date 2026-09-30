// Package indexer handles file indexing
package indexer

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	modSecTimestampLayout = "Mon Jan _2 15:04:05 2006"
)

type IndexBlock struct {
	TimestampStart int64
	TimestampEnd   int64
	StartOffset    int64
	EndOffset      int64
	BlockSize      uint32
}

func IndexFile(filePath string, blockSize uint32) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	var numLines uint32 = 0
	var offset int64 = 0

	blocks := make([]IndexBlock, 0)
	var block IndexBlock
	for {
		lineStart := offset

		lineBytes, readErr := reader.ReadBytes('\n')

		if len(lineBytes) > 0 {
			numLines++

			offset += int64(len(lineBytes))

			if numLines == 1 {
				timestamp, err := ReadTimestamp(lineBytes)
				if err != nil {
					return err
				}

				block = IndexBlock{
					TimestampStart: timestamp,
					StartOffset:    lineStart,
					BlockSize:      blockSize,
				}
			}

			if numLines == blockSize {
				timestamp, err := ReadTimestamp(lineBytes)
				if err != nil {
					return err
				}

				block.TimestampEnd = timestamp
				block.EndOffset = offset

				blocks = append(blocks, block)

				numLines = 0
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}

			return fmt.Errorf(
				"error reading file: %w",
				readErr,
			)
		}
	}

	indexPath := filePath + ".idx"

	return WriteIndexFile(indexPath, blocks)
}

func WriteIndexFile(indexPath string, blocks []IndexBlock) error {
	file, err := os.Create(indexPath)
	if err != nil {
		return fmt.Errorf("failed to create index file %q: %w", indexPath, err)
	}
	defer file.Close()

	for _, block := range blocks {
		if err := binary.Write(file, binary.LittleEndian, block); err != nil {
			return fmt.Errorf("failed to write index block: %w", err)
		}
	}

	return nil
}

func AppendIndexFile(indexPath string, blocks []IndexBlock) error {
	file, err := os.OpenFile(indexPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open index file %q for append: %w", indexPath, err)
	}
	defer file.Close()

	for _, block := range blocks {
		if err := binary.Write(file, binary.LittleEndian, block); err != nil {
			return fmt.Errorf("failed to append index block: %w", err)
		}
	}

	return nil
}

func ReadTimestamp(line []byte) (int64, error) {
	ts, ok := streamFindKey(line, "time_stamp")
	if !ok {
		return 0, fmt.Errorf("failed to read timestamp: %s", string(line))
	}

	timestamp, err := convertTimestamp(ts, modSecTimestampLayout)
	if err != nil {
		return 0, err
	}

	return timestamp, nil
}

func streamFindKey(jsonData []byte, targetKey string) (string, bool) {
	decoder := json.NewDecoder(bytes.NewReader(jsonData))

	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}

		if key, ok := token.(string); ok && key == targetKey {
			var value string
			if err := decoder.Decode(&value); err == nil {
				return value, true
			}
		}
	}
	return "", false
}

func convertTimestamp(raw, format string) (int64, error) {
	t, err := time.Parse(format, raw)
	if err != nil {
		return 0, err
	}

	return t.UnixNano(), nil
}
