package values

import (
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

const CoverageMetadata = "covmeta"
const CoverageCounters = "covcounters"

var CurrentFuzzDirName = "run_" + time.Now().Format("20060102_150405")

// GetCurrentFuzzDirName get Absolute path to the current fuzzing run directory
func GetCurrentFuzzDirName() string {
	return filepath.Join(viper.GetString(OutputDir), CurrentFuzzDirName)
}

func GetJsonOutputFileName() string {
	return filepath.Join(GetCurrentFuzzDirName(), "out.json")
}

func GetRequestLogFileName() string {
	return filepath.Join(GetCurrentFuzzDirName(), "requests.log")
}

func GetUnexpectedLogFileName() string {
	return filepath.Join(GetCurrentFuzzDirName(), "unexpected.log")
}
