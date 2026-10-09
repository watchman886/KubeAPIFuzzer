package utils

import (
	"fmt"
	"k-fuzz/values"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// CheckCoveragePath verifies that the given path:
// 1. Already exists.
// 2. Is a directory.
// 3. Contains two subdirectories: "covmeta" and "covcounters".
// Returns an error if any check fails.
func CheckCoveragePath() error {
	path := viper.GetString(values.CoverageFileDir)

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("coverage path does not exist: %s", path)
		}
		return fmt.Errorf("failed to stat coverage path %s: %v", path, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("coverage path is not a directory: %s", path)
	}

	covMetaDir := filepath.Join(path, values.CoverageMetadata)
	covCountersDir := filepath.Join(path, values.CoverageCounters)

	if _, err := os.Stat(covMetaDir); os.IsNotExist(err) {
		return fmt.Errorf("coverage metadata directory does not exist: %s", covMetaDir)
	}
	if _, err := os.Stat(covCountersDir); os.IsNotExist(err) {
		return fmt.Errorf("coverage counters directory does not exist: %s", covCountersDir)
	}

	return nil
}

// PruneKeepNewestFiles removes files in dir, keeping only the newest `keep` files
// by modification time. It ignores directories and any errors encountered while
// removing files (best-effort).
func PruneKeepNewestFiles(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	type fe struct {
		name string
		mod  time.Time
	}

	var files []fe
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fe{name: e.Name(), mod: info.ModTime()})
	}

	// If there are <= keep files, nothing to do.
	if len(files) <= keep {
		return nil
	}

	// sort by mod time descending (newest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].mod.After(files[j].mod)
	})

	// remove files after the first `keep` entries
	for i := keep; i < len(files); i++ {
		_ = os.Remove(filepath.Join(dir, files[i].name))
	}

	logrus.Infof("Pruned %d old coverage files in %s, kept %d newest", len(files)-keep, dir, keep)

	return nil
}
