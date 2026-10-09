package audit

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
)

type NodeIndexEntry struct {
	Identifier []byte
	Entry      int64
}

func (f Finding) WriteNodeFindingIndexEntry(dir string, offset int64) error {
	var node string
	var requestID string

	if f.Metadata.Request != nil {
		node = f.Metadata.Request.Metadata.Source
		requestID = f.Metadata.Request.Metadata.RequestID
	} else if f.Metadata.Response != nil {
		node = f.Metadata.Response.Metadata.Source
		requestID = f.Metadata.Response.Metadata.RequestID
	} else {
		return fmt.Errorf(
			"finding request and response is nil",
		)
	}

	if err := os.MkdirAll(
		dir,
		0o755,
	); err != nil {
		return fmt.Errorf(
			"failed to create node index directory: %w",
			err,
		)
	}

	filePath := filepath.Join(
		dir,
		fmt.Sprintf(
			"%s.idx",
			node,
		),
	)

	identifier := []byte(
		requestID,
	)

	if len(identifier) > math.MaxUint16 {
		return fmt.Errorf(
			"identifier length %d exceeds maximum supported length",
			len(identifier),
		)
	}

	file, err := os.OpenFile(
		filePath,
		os.O_RDWR|os.O_CREATE,
		0o644,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to open node index file: %w",
			err,
		)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf(
			"failed to stat node index file: %w",
			err,
		)
	}

	identifierLength := uint16(
		len(identifier),
	)

	if info.Size() == 0 {
		if err := binary.Write(
			file,
			binary.LittleEndian,
			identifierLength,
		); err != nil {
			return fmt.Errorf(
				"failed to write node index header: %w",
				err,
			)
		}
	} else {
		if _, err := file.Seek(
			0,
			io.SeekStart,
		); err != nil {
			return fmt.Errorf(
				"failed to seek node index header: %w",
				err,
			)
		}

		var storedLength uint16

		if err := binary.Read(
			file,
			binary.LittleEndian,
			&storedLength,
		); err != nil {
			return fmt.Errorf(
				"failed to read node index header: %w",
				err,
			)
		}

		if storedLength != identifierLength {
			return fmt.Errorf(
				"identifier length mismatch: index expects %d bytes, got %d",
				storedLength,
				identifierLength,
			)
		}
	}

	if _, err := file.Seek(
		0,
		io.SeekEnd,
	); err != nil {
		return fmt.Errorf(
			"failed to seek to end of node index file: %w",
			err,
		)
	}

	if _, err := file.Write(
		identifier,
	); err != nil {
		return fmt.Errorf(
			"failed to write node index identifier: %w",
			err,
		)
	}

	if err := binary.Write(
		file,
		binary.LittleEndian,
		offset,
	); err != nil {
		return fmt.Errorf(
			"failed to write node index offset: %w",
			err,
		)
	}

	return nil
}

func ReadNodeFindingIndexRange(filePath string, minOffset int64, maxOffset int64) (map[string][]int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to open node index file: %w",
			err,
		)
	}
	defer file.Close()

	var identifierLength uint16

	if err := binary.Read(
		file,
		binary.LittleEndian,
		&identifierLength,
	); err != nil {
		if err == io.EOF {
			return make(map[string][]int64), nil
		}

		return nil, fmt.Errorf(
			"failed to read node index header: %w",
			err,
		)
	}

	if identifierLength == 0 {
		return nil, fmt.Errorf(
			"invalid node index identifier length: 0",
		)
	}

	index := make(
		map[string][]int64,
	)

	for {
		identifier := make(
			[]byte,
			identifierLength,
		)

		_, err := io.ReadFull(
			file,
			identifier,
		)

		if err == io.EOF {
			break
		}

		if err == io.ErrUnexpectedEOF {
			return nil, fmt.Errorf(
				"truncated node index identifier",
			)
		}

		if err != nil {
			return nil, fmt.Errorf(
				"failed to read node index identifier: %w",
				err,
			)
		}

		var offset int64

		if err := binary.Read(
			file,
			binary.LittleEndian,
			&offset,
		); err != nil {
			if err == io.EOF ||
				err == io.ErrUnexpectedEOF {

				return nil, fmt.Errorf(
					"truncated node index offset",
				)
			}

			return nil, fmt.Errorf(
				"failed to read node index offset: %w",
				err,
			)
		}

		if offset < minOffset {
			continue
		}

		if offset > maxOffset {
			break
		}

		key := string(identifier)

		index[key] = append(
			index[key],
			offset,
		)
	}

	return index, nil
}
