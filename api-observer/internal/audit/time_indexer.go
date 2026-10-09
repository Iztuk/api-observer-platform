package audit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

type TimeIndexBlock struct {
	TimestampStart int64
	TimestampEnd   int64
	StartOffset    int64
	EndOffset      int64
	BlockSize      uint32
}

// Serves as the function for creating a sparse index based on time for the findings file
// Creates the boundary for searching entries within the findings log
func WatchTimeIndexFile(
	ctx context.Context,
	filePath string,
	blockSize uint32,
) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf(
			"failed to open findings file: %w",
			err,
		)
	}
	defer file.Close()

	indexPath := filePath + ".idx"

	lastBlock, err := LastIndexBlock(indexPath)
	if err != nil {
		return fmt.Errorf("failed to read last index block: %v", err)
	}

	offset := lastBlock.EndOffset

	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat findings file: %v", err)
	}

	if offset > fileInfo.Size() {
		return fmt.Errorf("index offset %d exceeds findings file size %d", offset, fileInfo.Size())
	}

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek findings file to offset %d", offset)
	}

	reader := bufio.NewReader(file)

	var numLines uint32
	var block TimeIndexBlock
	for {
		select {
		case <-ctx.Done():
			if numLines > 0 {
				block.BlockSize = numLines

				if err := AppendIndexFile(
					indexPath,
					[]TimeIndexBlock{block},
				); err != nil {
					return fmt.Errorf(
						"failed to flush partial index block: %w",
						err,
					)
				}
			}

			return nil

		default:
			lineStart := offset

			lineBytes, err := reader.ReadBytes('\n')

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
						100 * time.Millisecond,
					)

					continue
				}

				timestamp, timestampErr :=
					ReadTimestamp(lineBytes)
				if timestampErr != nil {
					return timestampErr
				}

				offset += int64(
					len(lineBytes),
				)

				numLines++

				if numLines == 1 {
					block = TimeIndexBlock{
						TimestampStart: timestamp,
						TimestampEnd:   timestamp,

						StartOffset: lineStart,
						EndOffset:   offset,

						BlockSize: blockSize,
					}
				} else {
					if timestamp <
						block.TimestampStart {

						block.TimestampStart =
							timestamp
					}

					if timestamp >
						block.TimestampEnd {

						block.TimestampEnd =
							timestamp
					}

					block.EndOffset = offset
				}

				if numLines == blockSize {
					if err := AppendIndexFile(
						indexPath,
						[]TimeIndexBlock{
							block,
						},
					); err != nil {
						return err
					}

					numLines = 0
					block = TimeIndexBlock{}
				}
			}

			if err != nil {
				if err == io.EOF {
					time.Sleep(
						100 * time.Millisecond,
					)

					continue
				}

				return fmt.Errorf(
					"failed to read findings file: %w",
					err,
				)
			}
		}
	}
}

func WriteIndexFile(indexPath string, blocks []TimeIndexBlock) error {
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

func AppendIndexFile(indexPath string, blocks []TimeIndexBlock) error {
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

func LastIndexBlock(
	indexPath string,
) (TimeIndexBlock, error) {
	file, err := os.Open(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return TimeIndexBlock{}, nil
		}

		return TimeIndexBlock{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return TimeIndexBlock{}, err
	}

	blockByteSize := int64(
		binary.Size(TimeIndexBlock{}),
	)

	if info.Size() == 0 {
		return TimeIndexBlock{}, nil
	}

	if info.Size()%blockByteSize != 0 {
		return TimeIndexBlock{},
			fmt.Errorf(
				"invalid index file size %d",
				info.Size(),
			)
	}

	lastOffset :=
		info.Size() - blockByteSize

	if _, err := file.Seek(
		lastOffset,
		io.SeekStart,
	); err != nil {
		return TimeIndexBlock{}, err
	}

	var block TimeIndexBlock

	if err := binary.Read(
		file,
		binary.LittleEndian,
		&block,
	); err != nil {
		return TimeIndexBlock{}, err
	}

	return block, nil
}

func ReadTimestamp(line []byte) (int64, error) {
	ts, ok := streamFindKey(line, "timestamp")
	if !ok {
		return 0, fmt.Errorf("failed to read timestamp: %s", string(line))
	}

	return ts, nil
}

func streamFindKey(jsonData []byte, targetKey string) (int64, bool) {
	decoder := json.NewDecoder(bytes.NewReader(jsonData))

	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}

		if key, ok := token.(string); ok && key == targetKey {
			var value int64
			if err := decoder.Decode(&value); err == nil {
				return value, true
			}

			return 0, false
		}
	}

	return 0, false
}
