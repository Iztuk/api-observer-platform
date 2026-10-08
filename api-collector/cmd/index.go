/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"api-collector/internal/indexer"
	"fmt"

	"github.com/spf13/cobra"
)

// indexCmd represents the index command
var filePath string
var blockSize uint32

var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Index a file",
	Long:  "API Collector indexes a file line by line for efficient querying on API Observer.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if blockSize == 0 {
			return fmt.Errorf("invalid block size: %d", blockSize)
		}

		return indexer.IndexFile(filePath, blockSize)
	},
}

func init() {
	rootCmd.AddCommand(indexCmd)

	indexCmd.Flags().StringVarP(
		&filePath,
		"path",
		"p",
		"",
		"File path to the log file to be indexed",
	)

	indexCmd.Flags().Uint32VarP(
		&blockSize,
		"block-size",
		"b",
		256,
		"Number of log lines per index block",
	)

	if err := indexCmd.MarkFlagRequired("path"); err != nil {
		fmt.Println(err)
	}

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// indexCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// indexCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
