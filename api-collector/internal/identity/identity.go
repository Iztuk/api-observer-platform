// Package identity handles unique identifiers for each collector.
package identity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

func GetOrCreateCollectorID() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf(
			"failed to get user config directory: %w",
			err,
		)
	}

	collectorDir := filepath.Join(
		configDir,
		"api-collector",
	)

	if err := os.MkdirAll(
		collectorDir,
		0o700,
	); err != nil {
		return "", fmt.Errorf(
			"failed to create collector config directory: %w",
			err,
		)
	}

	idPath := filepath.Join(
		collectorDir,
		"collector.id",
	)

	data, err := os.ReadFile(idPath)
	if err == nil {
		id := strings.TrimSpace(
			string(data),
		)

		if id == "" {
			return "", fmt.Errorf(
				"collector ID file is empty",
			)
		}

		if _, err := uuid.Parse(id); err != nil {
			return "", fmt.Errorf(
				"collector ID file contains an invalid UUID: %w",
				err,
			)
		}

		return id, nil
	}

	if !os.IsNotExist(err) {
		return "", fmt.Errorf(
			"failed to read collector ID: %w",
			err,
		)
	}

	id := uuid.NewString()

	if err := os.WriteFile(
		idPath,
		[]byte(id+"\n"),
		0o600,
	); err != nil {
		return "", fmt.Errorf(
			"failed to persist collector ID: %w",
			err,
		)
	}

	return id, nil
}
