package run

import (
	"k-fuzz/utils"
	"k-fuzz/values"
	"math/rand/v2"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// CompareCoverages compares the latest coverage counters file with the previous one.
// If the latest file covers more paths than the previous one, it returns true.
func (c *FuzzCtx) CompareCoverages() (bool, error) {
	latestCoverageFile, err := c.getLatestCoverageCounterFileName()
	if err != nil {
		return false, err
	}

	//logrus.Infof("previous coverage file name: %s", c.coverage.previousCoverageFileName)
	//logrus.Infof("latest coverage file name: %s", latestCoverageFile)

	// If there is no previous coverage file, we consider it as a new coverage
	if c.coverage.previousCoverageFileName == "" {
		c.coverage.previousCoverageFileName = latestCoverageFile
		return true, nil
	}

	// Prepare directories for coverage data processing
	covdata1Dir := filepath.Join(c.coverage.Dir, "covdata1")
	covdata2Dir := filepath.Join(c.coverage.Dir, "covdata2")
	covdatasubtractDir := filepath.Join(c.coverage.Dir, "covdatasubtract")

	// Clean up old covcounters files if they exist
	for _, dir := range []string{covdata1Dir, covdata2Dir, covdatasubtractDir} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false, err
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasPrefix(e.Name(), "covcounters.") {
				if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
					return false, err
				}
			}
		}
	}

	// Copy coverage files to respective directories
	if err := utils.CopyFile(c.coverage.previousCoverageFileName, filepath.Join(covdata1Dir, filepath.Base(c.coverage.previousCoverageFileName))); err != nil {
		c.coverage.previousCoverageFileName = latestCoverageFile
		return false, err
	}
	c.coverage.previousCoverageFileName = latestCoverageFile

	if err := utils.CopyFile(latestCoverageFile, filepath.Join(covdata2Dir, filepath.Base(latestCoverageFile))); err != nil {
		return false, err
	}

	goToolCovdataBinPath := viper.GetString(values.GoToolCovdataBinPath)

	// Run covdata subtract
	subtractCmd := newCovdataCommand(goToolCovdataBinPath, "subtract", "-i="+covdata2Dir+","+covdata1Dir, "-o="+covdatasubtractDir)
	if err := subtractCmd.Run(); err != nil {
		return false, err
	}

	// Add a timer to track go tool covdata textfmt duration
	startTime := time.Now()

	// Run go tool covdata textfmt
	covdatasubtractOut := filepath.Join(c.coverage.Dir, "covdatasubtract.out")

	// common args shared between the two invocation styles
	pkgs := "k8s.io/apiserver/...,k8s.io/apimachinery/...,k8s.io/kubernetes/plugin/pkg/...,k8s.io/kubernetes/pkg/registry/..."
	textfmtCmd := newCovdataCommand(goToolCovdataBinPath, "textfmt", "-pkg", pkgs, "-i="+covdatasubtractDir, "-o="+covdatasubtractOut)

	if err := textfmtCmd.Run(); err != nil {
		logrus.Errorf("go tool covdata textfmt command failed: %v", err)
		return false, err
	}

	duration := time.Since(startTime)
	logrus.Debugf("go tool covdata textfmt took %fs", duration.Seconds())

	// Count non-zero coverage lines
	countCmd := exec.Command("grep", "-v", "0$", covdatasubtractOut)
	output, err := countCmd.Output()
	if err != nil {
		// If grep returns exit code 1 (no matches), it means no new coverage
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}

	// Check if there are any non-empty lines (excluding the mode line)
	outputStr := strings.TrimSpace(string(output))
	hasNewCoverage := outputStr != "" && outputStr != "mode: atomic"

	// for testing purposes: print dirs in this function
	//logrus.Infof("covdata1Dir: %s", covdata1Dir)
	//logrus.Infof("covdata2Dir: %s", covdata2Dir)
	//logrus.Infof("covdatasubtractDir: %s", covdatasubtractDir)
	//
	//logrus.Infof("output of grep:\n%s", outputStr)

	return hasNewCoverage, nil
}

func newCovdataCommand(binPath string, subcommand string, args ...string) *exec.Cmd {
	if isGoCommand(binPath) {
		goArgs := append([]string{"tool", "covdata", subcommand}, args...)
		return exec.Command(binPath, goArgs...)
	}

	covdataArgs := append([]string{subcommand}, args...)
	return exec.Command(binPath, covdataArgs...)
}

func isGoCommand(binPath string) bool {
	cleaned := path.Base(strings.ReplaceAll(binPath, "\\", "/"))
	return cleaned == "go" || cleaned == "go.exe"
}

func (c *FuzzCtx) getLatestCoverageCounterFileName() (string, error) {
	var newestMod time.Time
	var newestPath string

	dir := filepath.Join(c.coverage.Dir, values.CoverageCounters)

	// 1% chance to prune old files when this function exits.
	if rand.IntN(100) == 0 {
		// defer pruning so it runs on function exit
		defer func(d string) {
			// keepNewest is 100 as requested
			keepNewest := 100
			err := utils.PruneKeepNewestFiles(d, keepNewest)

			if err != nil {
				logrus.Errorf("failed to prune old coverage files: %v", err)
			}
		}(dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	for _, e := range entries {
		// skip directories
		if e.IsDir() {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		if strings.HasPrefix(info.Name(), "covcounters.") && info.ModTime().After(newestMod) {
			newestMod = info.ModTime()
			newestPath = filepath.Join(dir, e.Name())
		}
	}
	return newestPath, nil
}

func isFileSizeSame(file1, file2 string) (bool, error) {
	info1, err := os.Stat(file1)
	if err != nil {
		return false, err
	}

	info2, err := os.Stat(file2)
	if err != nil {
		return false, err
	}

	return info1.Size() == info2.Size(), nil
}
