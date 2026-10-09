package utils

import (
	"encoding/json"
	"fmt"
	"k-fuzz/values"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

// CheckOutputFilePath verifies that the given path:
// 1. Does not already exist.
// 2. Its parent directory exists and is a directory.
// 3. The parent directory is writable.
// Returns an error if any check fails.
func CheckOutputFilePath() error {

	// Check if the output dir already exists
	if _, err := os.Stat(values.GetCurrentFuzzDirName()); err != nil {
		if !os.IsNotExist(err) {
			// Some other error (e.g., permission)
			return fmt.Errorf("failed to stat path %s: %v", values.GetCurrentFuzzDirName(), err)
		}
	} else {
		return fmt.Errorf("path already exists: %s", values.GetCurrentFuzzDirName())
	}

	// create dir recursively
	if err := os.MkdirAll(values.GetCurrentFuzzDirName(), 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %v", values.GetCurrentFuzzDirName(), err)
	}
	logrus.Infof("created directory %s", values.GetCurrentFuzzDirName())

	// Verify parent directory is writable by attempting to create and remove a temp file
	tmpFile := filepath.Join(values.GetCurrentFuzzDirName(), ".perm_test")
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY, 0666)
	defer func(f *os.File) {
		err := f.Close()
		if err != nil {
			logrus.Errorf("failed to close temp file: %v", err)
		}
	}(f)

	if err != nil {
		return fmt.Errorf("directory is not writable: %s", values.GetCurrentFuzzDirName())
	}
	if err := os.Remove(tmpFile); err != nil {
		return fmt.Errorf("failed to remove temp file: %v", err)
	}

	return nil
}

func WriteToOutputFile(fuzzResult []ResponseAnalyzeResult) error {
	file, err := os.OpenFile(values.GetJsonOutputFileName(),
		os.O_CREATE|os.O_WRONLY|os.O_EXCL, // create only if not exists
		0644)                              // owner read/write, others read-only
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			logrus.Errorf("failed to close file: %v", err)
		}
	}(file)

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(fuzzResult); err != nil {
		return fmt.Errorf("failed to write to file: %v", err)
	}

	logrus.Debugf("Fuzz result written to %s", values.GetJsonOutputFileName())
	return nil
}

func ReadFromOutputFile(filename string) ([]ResponseAnalyzeResult, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %v", err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			logrus.Errorf("failed to close file: %v", err)
		}
	}(file)

	var fuzzResult []ResponseAnalyzeResult
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&fuzzResult); err != nil {
		return nil, fmt.Errorf("failed to read from file: %v", err)
	}

	return fuzzResult, nil
}
