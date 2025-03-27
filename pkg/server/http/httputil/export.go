package httputil

import (
	"encoding/csv"
	"fmt"
	"net/http"
)

type exportType string

const (
	ExportTypeCSV exportType = "csv"
)

type Export struct {
	Type exportType `json:"type"`
}

func NewExport(t exportType) Export {
	return Export{
		Type: t,
	}
}

func (e Export) ExportHTTP(w http.ResponseWriter, headers []string, data []map[string]any, fileName string) error {
	// download the result as CSV
	if fileName == "" {
		fileName = "output.csv"
	}

	// Set the HTTP response Headers for CSV Download
	w.Header().Set(HeaderContentType, "text/csv")
	w.Header().Set(HeaderContentDisposition, `attachment; filename="`+fileName+`"`)

	// Create a CSV writer writing into the response
	writer := csv.NewWriter(w)

	// This assumes all maps have the same keys
	// Write CSV header
	if err := writer.Write(headers); err != nil {
		return err
	}

	// Iterate through data and write to the csv
	for _, recordMap := range data {
		var record []string
		for _, value := range headers { // use first item just for stable key ordering
			if val, ok := recordMap[value]; ok {
				record = append(record, fmt.Sprintf("%v", val))
			} else {
				record = append(record, "")
			}
		}
		if err := writer.Write(record); err != nil {
			return err
		}
	}

	writer.Flush()

	return nil
}
