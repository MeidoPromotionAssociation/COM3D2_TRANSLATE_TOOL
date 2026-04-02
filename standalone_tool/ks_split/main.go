// Split ks_extract CSV output into per-.ks CSV files.
//
// Reads the CSV produced by ks_extract (headers: 类型,voice_id,角色,所属arc,源文件,原文,译文)
// and writes one CSV per unique 源文件 value, containing only 原文,译文 columns.
//
// Usage:
//
//	ks_split <input.csv> [-o output_dir]
package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	inputPath, outputDir := parseArgs()

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Open(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening %s: %v\n", inputPath, err)
		os.Exit(1)
	}
	defer f.Close()

	// Skip UTF-8 BOM if present
	bom := make([]byte, 3)
	if n, _ := f.Read(bom); n == 3 && bom[0] == 0xEF && bom[1] == 0xBB && bom[2] == 0xBF {
		// BOM consumed, continue
	} else {
		// No BOM, seek back
		f.Seek(0, io.SeekStart)
	}

	reader := csv.NewReader(f)

	// Read header
	header, err := reader.Read()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading CSV header: %v\n", err)
		os.Exit(1)
	}

	// Find column indices
	colSource := indexOf(header, "源文件")
	colText := indexOf(header, "原文")
	colTrans := indexOf(header, "译文")
	if colSource < 0 || colText < 0 || colTrans < 0 {
		fmt.Fprintf(os.Stderr, "Error: CSV must have columns: 源文件, 原文, 译文\n")
		fmt.Fprintf(os.Stderr, "Found headers: %v\n", header)
		os.Exit(1)
	}

	// Group rows by source file, preserving order
	type fileData struct {
		rows [][]string
	}
	groups := make(map[string]*fileData)
	var order []string

	lineNum := 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading line %d: %v\n", lineNum+1, err)
			os.Exit(1)
		}
		lineNum++

		source := record[colSource]
		text := record[colText]
		trans := record[colTrans]

		if _, exists := groups[source]; !exists {
			groups[source] = &fileData{}
			order = append(order, source)
		}
		groups[source].rows = append(groups[source].rows, []string{text, trans})
	}

	// Write per-file CSVs
	totalFiles := 0
	totalRows := 0
	for _, source := range order {
		data := groups[source]

		// foo.ks -> foo.csv
		outName := strings.TrimSuffix(source, filepath.Ext(source)) + ".csv"
		outPath := filepath.Join(outputDir, outName)

		of, err := os.Create(outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating %s: %v\n", outPath, err)
			continue
		}

		// Write UTF-8 BOM
		of.Write([]byte{0xEF, 0xBB, 0xBF})

		w := csv.NewWriter(of)
		w.Write([]string{"原文", "译文"})
		for _, row := range data.rows {
			w.Write(row)
		}
		w.Flush()
		of.Close()

		fmt.Printf("  %s: %d entries\n", outName, len(data.rows))
		totalFiles++
		totalRows += len(data.rows)
	}

	fmt.Printf("\nSplit %d entries into %d files -> %s\n", totalRows, totalFiles, outputDir)
}

func indexOf(slice []string, target string) int {
	for i, s := range slice {
		if s == target {
			return i
		}
	}
	return -1
}

func parseArgs() (inputPath, outputDir string) {
	outputDir = "split_output"
	args := os.Args[1:]

	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 < len(args) {
				outputDir = args[i+1]
				i++
			}
		case "-h", "--help":
			printUsage()
			os.Exit(0)
		default:
			if strings.HasPrefix(args[i], "-o=") {
				outputDir = args[i][3:]
			} else if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(os.Stderr, "Unknown flag: %s\n", args[i])
				printUsage()
				os.Exit(1)
			} else {
				positional = append(positional, args[i])
			}
		}
	}

	if len(positional) < 1 {
		printUsage()
		os.Exit(1)
	}
	inputPath = positional[0]
	return
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: %s <input.csv> [-o output_dir]\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Split ks_extract CSV into per-.ks CSV files (原文,译文)\n\n")
	fmt.Fprintf(os.Stderr, "Flags:\n")
	fmt.Fprintf(os.Stderr, "  -o string\n\tOutput directory (default \"split_output\")\n")
}
