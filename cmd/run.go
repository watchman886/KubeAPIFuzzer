package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"k-fuzz/app/run"
	"k-fuzz/utils"
	"k-fuzz/values"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const ScoreThreshold = 20

type RunCmdCtx struct {
	ctx                   context.Context
	alterSucceedCount     int
	alterNewType422Count  int
	alterSameType422Count int
	alterFailedCount      int
	httpStatusCodeCount   map[int]int
	coverage              run.Coverage
	features              run.FeatureFlags
	pathValueCache        *run.PathValueCache
	seedCorpus            *utils.RequestQueue
	requestLogger         *logrus.Logger
	unexpectedLogger      *logrus.Logger
}

var runCmd = &cobra.Command{
	Use:   "run [file]",
	Short: "Run fuzz test",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return cmd.Help()
		}

		viper.Set(values.Host, sharedParams.host)
		viper.Set(values.BearerToken, sharedParams.bearerToken)
		viper.Set(values.SkipTlsVerify, sharedParams.skipTlsVerify)

		// check if values.GoToolCovdataBinPath exists
		if viper.GetString(values.GoToolCovdataBinPath) != "go" {
			if _, err := os.Stat(viper.GetString(values.GoToolCovdataBinPath)); os.IsNotExist(err) {
				return fmt.Errorf("go tool covdata binary does not exist: %s", viper.GetString(values.GoToolCovdataBinPath))
			}
		}

		if err := utils.CheckOutputFilePath(); err != nil {
			return err
		}
		if err := utils.CheckCoveragePath(); err != nil {
			return err
		}

		requestLogger, err := newFileLogger(values.GetRequestLogFileName(), false)
		if err != nil {
			return err
		}

		// logger for unexpected errors, useful in debugging
		unexpectedLogger, err := newFileLogger(values.GetUnexpectedLogFileName(), true)
		if err != nil {
			return err
		}

		err = initCoverageData()
		if err != nil {
			return fmt.Errorf("failed to initialize coverage data: %v", err)
		}

		filename := args[0]
		loader := openapi3.NewLoader()
		doc, err := loader.LoadFromFile(filename)
		if err != nil {
			return err
		}

		_, err = url.Parse(viper.GetString(values.Host))
		if err != nil {
			logrus.Fatal(err)
		}

		c, cancel := createKeyboardInterruptableContext(context.Background())
		defer cancel()

		var sensitiveRequests []utils.ResponseAnalyzeResult

		workerExitedNormally := make(chan struct{})
		go func() {
			defer close(workerExitedNormally)

			ctx := RunCmdCtx{
				ctx:                 c,
				alterSucceedCount:   0,
				httpStatusCodeCount: make(map[int]int),
				coverage: run.Coverage{
					Dir: viper.GetString(values.CoverageFileDir),
				},
				features: run.FeatureFlags{
					Enable422Repair:        viper.GetBool(values.Enable422Repair),
					EnableStateAwareness:   viper.GetBool(values.EnableStateAwareness),
					EnableCoverageFeedback: viper.GetBool(values.EnableCoverageFeedback),
				},
				pathValueCache:   run.NewPathValueCache(),
				seedCorpus:       utils.NewRequestQueue(),
				requestLogger:    requestLogger,
				unexpectedLogger: unexpectedLogger,
			}

			logrus.Infof("Feature switches: 422-repair=%t, state-awareness=%t, coverage-feedback=%t",
				ctx.features.Enable422Repair,
				ctx.features.EnableStateAwareness,
				ctx.features.EnableCoverageFeedback,
			)

			start := time.Now()
			sensitiveRequests = ctx.firstTimeCoverage(doc)
			firstTimeCoverageDuration := time.Since(start)

			requestLogger.Infof("Fisrt time coverage finished")
			logrus.Infof("Alter metrics: succeeded=%d, new-type-422=%d, same-type-422=%d, failed=%d", ctx.alterSucceedCount, ctx.alterNewType422Count, ctx.alterSameType422Count, ctx.alterFailedCount)

			start = time.Now()
			mutateResult := ctx.mutate()
			mutateDuration := time.Since(start)

			sensitiveRequests = append(sensitiveRequests, mutateResult...)

			logrus.Debugf("Successfully altered %d requests", ctx.alterSucceedCount)
			logrus.Debugf("Seed corpus queue remaining size: %d", ctx.seedCorpus.Len())

			logrus.Debugf("First time coverage duration: %dm%ds", int(firstTimeCoverageDuration.Minutes()), int(firstTimeCoverageDuration.Seconds())%60)
			logrus.Debugf("Mutation duration: %dm%ds", int(mutateDuration.Minutes()), int(mutateDuration.Seconds())%60)
		}()

		<-workerExitedNormally

		if err := utils.WriteToOutputFile(sensitiveRequests); err != nil {
			return err
		}

		return nil
	},
}

func init() {
	runCmd.Flags().Int64P(values.Seed, "s", 10694, "fuzz random seed")
	runCmd.Flags().StringVarP(&sharedParams.host, values.Host, "H", "", "target host")
	runCmd.Flags().BoolVar(&sharedParams.skipTlsVerify, values.SkipTlsVerify, false, "skip TLS verify")
	// in k8s, "watch=true" in request body may cause expected timeout
	runCmd.Flags().Uint64(values.Timeout, 1, "timeout in seconds")
	runCmd.Flags().Bool(values.DryRun, false, "send request to local mock server, which only return 200 OK")
	runCmd.Flags().Uint(values.MaxSchemaDepth, 20, "maximum depth when traversing the schema")
	runCmd.Flags().Bool(values.RandomFill, false, "randomly fill schema")
	runCmd.Flags().StringVar(&sharedParams.bearerToken, values.BearerToken, "", "bearer token")
	runCmd.Flags().StringP(values.CoverageFileDir, "c", "", "directory to store coverage files")
	runCmd.Flags().Uint(values.MaxMutateLoopCount, 10, "maximum number of mutation loop")
	runCmd.Flags().StringP(values.OutputDir, "o", "", "output file dir")
	runCmd.Flags().StringP(values.GoToolCovdataBinPath, "b", "", "path to go tool covdata binary; if not custom go-tool-covdata, use 'go'")
	runCmd.Flags().Bool(values.Enable422Repair, true, "enable adaptive repair for 422 responses")
	runCmd.Flags().Bool(values.EnableStateAwareness, true, "enable runtime state awareness and state reuse")
	runCmd.Flags().Bool(values.EnableCoverageFeedback, true, "enable coverage-guided seed retention")

	if err := viper.BindPFlags(runCmd.Flags()); err != nil {
		logrus.Fatalf("failed to bind flags: %v", err)
	}

	if err := runCmd.MarkFlagRequired(values.Host); err != nil {
		panic(err)
	}
	if err := runCmd.MarkFlagRequired(values.CoverageFileDir); err != nil {
		panic(err)
	}
	if err := runCmd.MarkFlagRequired(values.OutputDir); err != nil {
		panic(err)
	}
	if err := runCmd.MarkFlagRequired(values.GoToolCovdataBinPath); err != nil {
		panic(err)
	}

	rootCmd.AddCommand(runCmd)
}

// sendWithRetries sends a request up to 5 times, retrying on 422 by adjusting the body.
func (c *RunCmdCtx) sendWithRetries(ctx run.FuzzCtx, client *http.Client, schema utils.RequestSchema, requestData utils.RequestData) (*http.Response, []byte, utils.RequestData, error) {
	var resp *http.Response
	var respBody []byte
	var err error
	var prevReason string

	for attempt := 1; attempt <= 5; attempt++ {
		logrus.Debugf("Sending request attempt %d: %s %s", attempt, *requestData.Method, *requestData.Path)

		var causesString string

		start := time.Now()
		resp, respBody, err = ctx.Request(client, schema, requestData)
		requestDuration := time.Since(start)
		logrus.Debugf("Request time %s", requestDuration)

		if requestDuration > 1*time.Second && requestData.Params["watch"] != "true" {
			marshalledRequest, _ := json.Marshal(requestData)
			c.unexpectedLogger.Warnf("request took longer than 1 seconds: %s, request data: %s, response: %s", requestDuration, string(marshalledRequest), string(respBody))
		}

		if err != nil {
			logrus.Warnf("Request attempt %d %s %s failed: %v", attempt, *requestData.Method, *requestData.Path, err)
			return resp, respBody, requestData, err
		}

		if resp.StatusCode == http.StatusUnprocessableEntity {
			if !c.features.Enable422Repair {
				break
			}

			logrus.Warnf("Attempt %d unprocessable (422). Body: %s", attempt, string(requestData.BodyBytes))

			requestData.BodyBytes, causesString, err = ctx.AdjustRequestBody(schema.ApiSpec.RequestBody, requestData.BodyBytes, respBody)
			if err != nil {
				logrus.Warnf("Adjust request body failed on attempt %d: %v", attempt, err)
				return resp, respBody, requestData, err
			}
			logrus.Debugf("Altered body for retry %d", attempt)

			// if the cause is the same as previous, stop retrying
			if causesString == prevReason {
				c.alterSameType422Count += 1
				break
			}
			prevReason = causesString

			continue
		}

		// metrics for 422 handling
		if attempt > 1 {
			if resp.StatusCode == http.StatusUnprocessableEntity {
				c.alterNewType422Count += 1
			} else {
				if http.StatusOK <= resp.StatusCode && resp.StatusCode < http.StatusMultipleChoices {
					c.alterSucceedCount += 1
				} else {
					c.alterFailedCount += 1
				}
			}
		}

		// stop retrying on success or other status codes
		break
	}
	return resp, respBody, requestData, nil
}

func extractKindAndAPIVersion(op *openapi3.Operation) (string, string) {
	if op == nil {
		return "", ""
	}

	k8sGroupVersionKind, ok := op.Extensions["x-kubernetes-group-version-kind"]
	if !ok {
		return "", ""
	}

	m, ok := k8sGroupVersionKind.(map[string]any)
	if !ok {
		return "", ""
	}

	kind, _ := m["kind"].(string)
	group, _ := m["group"].(string)
	version, _ := m["version"].(string)

	if version == "" {
		return kind, ""
	}
	if group == "" {
		return kind, version
	}

	return kind, group + "/" + version
}

// fuzzOperation prepares and sends a fuzzed request for a single operation.
func (c *RunCmdCtx) fuzzOperation(path string, method string, schema utils.RequestSchema) (*http.Response, []byte, utils.RequestData, error) {

	client := utils.NewClient()

	kind, apiVersion := extractKindAndAPIVersion(schema.ApiSpec)
	ctx := run.NewFuzz(kind, apiVersion, c.pathValueCache, c.httpStatusCodeCount, &c.coverage, c.seedCorpus, c.requestLogger, c.unexpectedLogger, false, c.features)

	// fill path parameters
	filledPath, err := ctx.FuzzPath(path)
	if err != nil {
		return nil, nil, utils.RequestData{}, err
	}

	// fuzz query and headers
	params := ctx.FuzzParams(schema.ApiSpec, method, path)
	// fuzz body schema
	body, contentType, err := ctx.FuzzRequestBody(schema.ApiSpec)
	if err != nil {
		logrus.Warnf("Fuzz request body failed: %v", err)
		return nil, nil, utils.RequestData{}, err
	}

	// send with retry logic
	resp, respBody, reqData, err := c.sendWithRetries(ctx, client, schema,
		utils.RequestData{
			Method:       &method,
			Path:         &filledPath,
			PathTemplate: &path,
			Params:       params,
			BodyBytes:    body,
			ContentType:  &contentType,
			Kind:         &kind,
		})
	return resp, respBody, reqData, err
}

// firstTimeCoverage iterates all API paths and operations, fuzzing each and collecting sensitive requests.
func (c *RunCmdCtx) firstTimeCoverage(doc *openapi3.T) []utils.ResponseAnalyzeResult {
	sensitiveRequests := make([]utils.ResponseAnalyzeResult, 0)

	for path, pathItem := range doc.Paths.Map() {
		select {
		case <-c.ctx.Done():
			logrus.Infof("Context cancelled, stopping first time coverage")
			return sensitiveRequests
		default:
			logrus.Debugf("Fuzzing API path: %s", path)
			opsMap := pathItem.Operations()

			runMethod := func(method string) {
				if op, ok := opsMap[method]; ok {
					if utils.HasWatchSegment(path) {
						logrus.Infof("Skipping watch path %s", path)
						return
					}
					resp, respBody, reqData, err := c.fuzzOperation(path, method, utils.RequestSchema{ApiSpec: op, SharedParams: pathItem.Parameters})
					if err != nil {
						return
					}
					score := utils.ScoreResponse(resp, respBody)
					logrus.Debugf("Request %s %s scored %d", method, *reqData.Path, score)
					if score > ScoreThreshold {
						sensitiveRequests = append(sensitiveRequests, utils.ResponseAnalyzeResult{
							Req: &utils.Request{
								Data: &reqData,
								Schema: &utils.RequestSchema{
									ApiSpec:      op,
									SharedParams: pathItem.Parameters,
								},
							},
							Resp: &utils.ResponseData{
								StatusCode: resp.StatusCode,
								Body:       respBody,
							},
							Time: time.Now(),
						})
					}
				}
			}

			// 1. Create
			runMethod(http.MethodPost)
			// 2. Read
			runMethod(http.MethodGet)
			// 3. Update
			runMethod(http.MethodPut)
			runMethod(http.MethodPatch)
			// 4. Other methods
			for method := range opsMap {
				switch method {
				case http.MethodPost, http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete:
					// CRUD have been handled above, skip
				default:
					runMethod(method)
				}
			}
			// 5. Delete
			runMethod(http.MethodDelete)
		}
	}

	return sensitiveRequests
}

func storeMutateResult(originalRequest utils.Request, reqDataAfterRetries utils.RequestData, resp *http.Response, respBody []byte, sensitiveRequests *[]utils.ResponseAnalyzeResult) {
	score := utils.ScoreResponse(resp, respBody)
	logrus.Debugf("Request %s %s scored %d", *originalRequest.Data.Method, *reqDataAfterRetries.Path, score)
	if score > ScoreThreshold {
		*sensitiveRequests = append(*sensitiveRequests, utils.ResponseAnalyzeResult{
			Req: &utils.Request{
				Data: &reqDataAfterRetries,
				Schema: &utils.RequestSchema{
					ApiSpec:      originalRequest.Schema.ApiSpec,
					SharedParams: originalRequest.Schema.SharedParams,
				},
			},
			Resp: &utils.ResponseData{
				StatusCode: resp.StatusCode,
				Body:       respBody,
			},
			Time: time.Now(),
		})
	}
}

func (c *RunCmdCtx) mutate() []utils.ResponseAnalyzeResult {
	client := utils.NewClient()
	sensitiveRequests := make([]utils.ResponseAnalyzeResult, 0)

	mutateLoopCount := uint(0)
	maxMutateLoopCount := viper.GetUint(values.MaxMutateLoopCount)

	for c.seedCorpus.Len() > 0 && mutateLoopCount < maxMutateLoopCount {
		request, _ := c.seedCorpus.Dequeue()

		// create a map of parameter schema references for mutation
		paramSchemaMap := make(map[string]*openapi3.ParameterRef)
		for _, paramRef := range request.Schema.SharedParams {
			if paramRef != nil && paramRef.Value != nil {
				paramSchemaMap[paramRef.Value.Name] = paramRef
			}
		}
		for _, paramRef := range request.Schema.ApiSpec.Parameters {
			if paramRef != nil && paramRef.Value != nil {
				paramSchemaMap[paramRef.Value.Name] = paramRef
			}
		}

		kind, apiVersion := extractKindAndAPIVersion(request.Schema.ApiSpec)
		ctx := run.NewFuzz(kind, apiVersion, c.pathValueCache, c.httpStatusCodeCount, &c.coverage, c.seedCorpus, c.requestLogger, c.unexpectedLogger, true, c.features)

		for i := 0; i < 256; i += 1 {
			select {
			case <-c.ctx.Done():
				logrus.Infof("Context cancelled, stopping mutation")
				utils.PrintStatusReport(c.httpStatusCodeCount)
				return sensitiveRequests
			default:
				logrus.Debugf("Mutating request: %s %s, iteratoin: %v", *request.Data.Method, *request.Data.Path, i)

				mutatedBody := ctx.MutateBody(request.Data.BodyBytes, request.Schema.ApiSpec.RequestBody)

				logrus.Debugf("Mutated body of: %s %s", *request.Data.Method, *request.Data.Path)

				mutatedParams := ctx.MutateParams(request.Data.Params, paramSchemaMap)

				logrus.Debugf("Mutated params of: %s %s", *request.Data.Method, *request.Data.Path)

				resp, respBody, reqDataAfterRetries, err := c.sendWithRetries(ctx, client, *request.Schema, utils.RequestData{
					Method:       request.Data.Method,
					Path:         request.Data.Path,
					PathTemplate: request.Data.PathTemplate,
					Params:       mutatedParams,
					BodyBytes:    mutatedBody,
					ContentType:  request.Data.ContentType,
					Kind:         request.Data.Kind,
				})

				if err != nil {
					logrus.Warnf("Failed to send request: %v", err)
					continue
				}

				storeMutateResult(request, reqDataAfterRetries, resp, respBody, &sensitiveRequests)
			}
		}

		// c.seedCorpus.GetRandRequest()
		// use the path and method of this random request, to mutate the current request
		for i := 0; i < 8; i += 1 {
			select {
			case <-c.ctx.Done():
				logrus.Infof("Context cancelled, stopping mutation")
				utils.PrintStatusReport(c.httpStatusCodeCount)
				return sensitiveRequests
			default:
				randomRequest, ok := c.seedCorpus.GetRandRequest()
				if !ok {
					logrus.Debugf("No more requests in seed corpus, stopping mutation")
					break
				}

				newMethod := *randomRequest.Data.Method
				if utils.R.IntN(2) == 0 {
					newMethod = *request.Data.Method // 50% chance to keep the original method
				}

				// newPath has a 50% chance to be the same as the original request path
				newPath := *request.Data.Path
				newPathTemplate := request.Data.PathTemplate
				if utils.R.IntN(2) == 0 {
					newPath = *randomRequest.Data.Path
					newPathTemplate = randomRequest.Data.PathTemplate
				}

				resp, respBody, reqDataAfterRetries, err := c.sendWithRetries(ctx, client, *randomRequest.Schema,
					utils.RequestData{
						Method:       &newMethod,
						Path:         &newPath,
						PathTemplate: newPathTemplate,
						Params:       request.Data.Params,
						BodyBytes:    request.Data.BodyBytes, // keep the original body
						ContentType:  randomRequest.Data.ContentType,
						Kind:         randomRequest.Data.Kind,
					})

				if err != nil {
					logrus.Warnf("Failed to send request: %v", err)
					continue
				}

				storeMutateResult(request, reqDataAfterRetries, resp, respBody, &sensitiveRequests)
			}
		}

		mutateLoopCount += 1
	}

	utils.PrintStatusReport(c.httpStatusCodeCount)

	return sensitiveRequests
}

func newFileLogger(filePath string, needSourceCodeLocation bool) (*logrus.Logger, error) {
	file, err := os.OpenFile(filePath,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o666)
	if err != nil {
		return nil, err
	}

	logger := logrus.New()
	logger.SetOutput(file)
	logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
	})
	logger.SetLevel(logrus.InfoLevel)

	if !needSourceCodeLocation {
		logger.SetReportCaller(true)
		logger.SetFormatter(&logrus.TextFormatter{
			// add a space between the log level and the message
			CallerPrettyfier: func(frame *runtime.Frame) (function string, file string) {
				return frame.Function, fmt.Sprintf(" %s:%d", frame.File, frame.Line)
			},
		})
	}

	return logger, nil
}

// initCoverageData initializes the coverage data directories and copies the latest covmeta file
func initCoverageData() error {
	// Create required directories
	dirs := []string{"covdata1", "covdata2", "covdatasubtract"}
	for _, dir := range dirs {
		dirPath := filepath.Join(viper.GetString(values.CoverageFileDir), dir)
		// Remove directory if it exists
		if _, err := os.Stat(dirPath); err == nil {
			if err := os.RemoveAll(dirPath); err != nil {
				return err
			}
		}
		// Create directory
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return err
		}
	}

	// Copy the latest covmeta file to covdata1 and covdata2 directories
	latestCovmeta, err := getLatestCovmetaFileName()
	if err != nil {
		return err
	}

	if latestCovmeta != "" {
		dest1 := filepath.Join(viper.GetString(values.CoverageFileDir), "covdata1", filepath.Base(latestCovmeta))
		dest2 := filepath.Join(viper.GetString(values.CoverageFileDir), "covdata2", filepath.Base(latestCovmeta))

		if err := utils.CopyFile(latestCovmeta, dest1); err != nil {
			return err
		}
		if err := utils.CopyFile(latestCovmeta, dest2); err != nil {
			return err
		}
	}

	return nil
}

// getLatestCovmetaFileName returns the path of the latest covmeta file
func getLatestCovmetaFileName() (string, error) {
	var newestMod time.Time
	var newestPath string

	dir := filepath.Join(viper.GetString(values.CoverageFileDir), values.CoverageMetadata)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	for _, e := range entries {
		// skip directories
		if e.IsDir() {
			continue
		}

		// only consider files that start with "covmeta."
		if !strings.HasPrefix(e.Name(), "covmeta.") {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		if info.ModTime().After(newestMod) {
			newestMod = info.ModTime()
			newestPath = filepath.Join(dir, e.Name())
		}
	}
	return newestPath, nil
}
