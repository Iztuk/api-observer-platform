package indexer

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

func ReadIndexFile(filePath string) ([]IndexBlock, error) {
	indexPath := filePath + ".idx"
	file, err := os.Open(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open index file %q: %w", indexPath, err)
	}
	defer file.Close()
	blocks := make([]IndexBlock, 0)
	for {
		var block IndexBlock
		err := binary.Read(file, binary.LittleEndian, &block)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to read index block: %w", err)
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}
