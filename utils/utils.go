package utils

import (
	"io"
	"os"

	"github.com/getkin/kin-openapi/openapi3"
)

func GetAType(types *openapi3.Types) string {
	if types == nil {
		return ""
	}

	for _, t := range *types {
		if t != "" && t != "null" {
			return t
		}
	}

	return ""
}

// CopyFile copies a file from src to dst
func CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	return destFile.Sync()
}
