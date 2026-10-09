package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"k-fuzz/utils"
	"k-fuzz/values"
	"net/url"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type K8s5xxErrorOutput struct {
	RequestPaths      []string `json:"request_path"`
	FilteredCallStack string   `json:"filtered_call_stack"`
}

var replayCmd = &cobra.Command{
	Use:   "replay [file]",
	Short: "Replay requests that caused 500 responses, then output filtered call stacks",
	RunE: func(cmd *cobra.Command, args []string) error {

		if len(args) < 1 {
			return cmd.Help()
		}

		viper.Set(values.Host, sharedParams.host)
		viper.Set(values.OutputFile, sharedParams.outPutFile)
		viper.Set(values.BearerToken, sharedParams.bearerToken)
		viper.Set(values.SkipTlsVerify, sharedParams.skipTlsVerify)

		file := args[0]

		// prepare output (file or stdout)
		out, cleanup, err := prepareOutput(viper.GetString(values.OutputFile))
		if err != nil {
			return err
		}
		defer cleanup()

		// TODO: implement keyboard interrupt handling
		c, cancel := createKeyboardInterruptableContext(context.Background())
		defer cancel()

		_, err = url.Parse(viper.GetString(values.Host))
		if err != nil {
			logrus.Fatal(err)
		}

		// check file existence
		if _, err := os.Stat(file); os.IsNotExist(err) {
			logrus.Fatalf("fuzz log file does not exist: %s", file)
		}
		if _, err := os.Stat(viper.GetString(values.K8sOutputFile)); os.IsNotExist(err) {
			logrus.Fatalf("k8s output file does not exist: %s", viper.GetString(values.K8sOutputFile))
		}

		filteredResult := replay(c, file, viper.GetString(values.K8sOutputFile))

		err = outputK8sOutputFilteredResult(filteredResult, out)
		if err != nil {
			return err
		}

		return nil
	},
}

func init() {
	replayCmd.Flags().String(values.K8sOutputFile, "", "k8s output file")
	replayCmd.Flags().StringVarP(&sharedParams.outPutFile, values.OutputFile, "o", "", "output file")
	replayCmd.Flags().StringVarP(&sharedParams.host, values.Host, "H", "", "target host")
	replayCmd.Flags().StringVar(&sharedParams.bearerToken, values.BearerToken, "", "bearer token")
	replayCmd.Flags().BoolVar(&sharedParams.skipTlsVerify, values.SkipTlsVerify, false, "skip TLS verify")

	if err := viper.BindPFlags(replayCmd.Flags()); err != nil {
		logrus.Fatalf("failed to bind flags: %v", err)
	}

	if err := replayCmd.MarkFlagRequired(values.Host); err != nil {
		panic(err)
	}
	if err := replayCmd.MarkFlagRequired(values.K8sOutputFile); err != nil {
		panic(err)
	}
	if err := replayCmd.MarkFlagRequired(values.OutputFile); err != nil {
		panic(err)
	}
	rootCmd.AddCommand(replayCmd)
}

// in-memory offsets for files we've read; protected by mutex.
var (
	offsetsMu   sync.Mutex
	fileOffsets = make(map[string]int64)
)

func replay(c context.Context, fuzzLog, k8sOutputFile string) []K8s5xxErrorOutput {
	// read fuzzLog to []utils.ResponseAnalyzeResult
	logrus.Infof("starting to read fuzz log: %s", fuzzLog)
	responses, err := utils.ReadFromOutputFile(fuzzLog)
	if err != nil {
		logrus.Fatalf("failed to read fuzz log: %v", err)
	}
	logrus.Infof("finished reading fuzz log: %s, total responses: %d", fuzzLog, len(responses))

	client := utils.NewClient()
	count := 0

	// traverse responses, for each with StatusCode 5xx, send request again
	result := make(map[string]K8s5xxErrorOutput)
	for _, resp := range responses {
		if resp.Resp == nil || resp.Req == nil || resp.Req.Data == nil {
			continue
		}
		if resp.Resp.StatusCode >= 500 && resp.Resp.StatusCode < 600 {
			count++

			// replay request
			newResp, _, err := utils.SendRequest(client, *resp.Req.Data)
			if err != nil {
				logrus.Errorf("failed to replay request for path %s: %v", *resp.Req.Data.Path, err)
				continue
			}

			if newResp.StatusCode < 500 || newResp.StatusCode >= 600 {
				logrus.Warnf("fuzz path %s response status code: %d", *resp.Req.Data.Path, newResp.StatusCode)
				continue
			}

			// filter stack trace: read only new content since last call
			currStackTrace, err := CollectNextLinesAfterRequestScopeErr(k8sOutputFile)
			if err != nil {
				logrus.Errorf("failed to collect call stack: %v", err)
				continue
			}
			if len(currStackTrace) == 0 {
				logrus.Errorf("failed to collect call stack: empty stack trace")
			}
			for _, v := range currStackTrace {
				// fill in result
				if existing, ok := result[v]; ok {
					existing.RequestPaths = append(existing.RequestPaths, *resp.Req.Data.Path)
					result[v] = existing
				} else {
					result[v] = K8s5xxErrorOutput{
						RequestPaths:      []string{*resp.Req.Data.Path},
						FilteredCallStack: v,
					}
				}
			}

		}
	}

	logrus.Infof("total 5xx responses replayed: %d", count)

	// convert map to slice
	filteredResult := make([]K8s5xxErrorOutput, 0, len(result))
	for _, v := range result {
		filteredResult = append(filteredResult, v)
	}

	return filteredResult
}

func outputK8sOutputFilteredResult(filtered []K8s5xxErrorOutput, out io.Writer) error {
	logrus.WithFields(logrus.Fields{
		"count": len(filtered),
	}).Info("Filtered k8s error responses with status code 5xx:")

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(filtered)
}

// CollectNextLinesAfterRequestScopeErr reads the file at filename and returns a slice of strings,
// each being the full line that follows a line containing the marker
// "handlers.(*RequestScope).err". It will start reading from the last recorded offset for this
// filename (if any), read until EOF, and update the offset to the new EOF. If the file has been
// truncated/rotated (current offset > file size), it will start from the beginning.
func CollectNextLinesAfterRequestScopeErr(filename string) ([]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// determine file size
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := stat.Size()

	// get last offset
	offsetsMu.Lock()
	offset := fileOffsets[filename]
	offsetsMu.Unlock()

	// If offset is beyond current size (e.g. truncated/rotated), start at 0
	if offset > size {
		offset = 0
	}

	// seek to last offset
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(f)
	results := make([]string, 0)
	const marker = "handlers.(*RequestScope).err"

	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, marker) {
			// Advance up to 3 lines and capture the 3rd line after the marker.
			var thirdLine string
			got := 0
			for i := 0; i < 3; i++ {
				if scanner.Scan() {
					got++
					if i == 2 {
						thirdLine = scanner.Text()
					}
				} else {
					// Reached EOF before getting 3 lines; stop processing further.
					break
				}
			}
			if got >= 3 {
				// strip leading/trailing whitespace
				thirdLine = strings.TrimSpace(thirdLine)
				results = append(results, thirdLine)
			} else {
				// Not enough lines after marker; break out similar to previous behavior.
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// update offset to current position (EOF or where scanner stopped)
	newOffset, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	offsetsMu.Lock()
	fileOffsets[filename] = newOffset
	offsetsMu.Unlock()

	return results, nil
}

// TruncateFileContents truncates the file at filename to zero length without removing the file.
// It attempts to acquire an advisory exclusive lock (with short retries) to reduce races
// with other writers; this is best-effort and may not prevent all races if writers don't use locks.
func TruncateFileContents(filename string) error {
	// Open the file for read-write so we can lock and truncate.
	f, err := os.OpenFile(filename, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	fd := int(f.Fd())

	// WARNING: Currently, the other process does not use file locking.
	// The file locking here is for future compatibility.

	// Try to acquire exclusive lock with retries for up to 2 seconds.
	locked := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			locked = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Even if locking failed, proceed to truncate (best-effort).
	if err := f.Truncate(0); err != nil {
		if locked {
			_ = syscall.Flock(fd, syscall.LOCK_UN)
		}
		return err
	}
	// Ensure filesystem state is updated.
	err = f.Sync()
	if err != nil {
		logrus.Warnf("failed to sync file %s after truncation: %v", filename, err)
	}

	if locked {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
	}
	return nil
}
