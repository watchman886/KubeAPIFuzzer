package run

import (
	"encoding/json"
	"fmt"
	"k-fuzz/utils"
	"regexp"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/sirupsen/logrus"
)

// Status captures the validation errors from the kube-apiserver response.
type Status struct {
	Details struct {
		Causes []struct {
			Reason  string `json:"reason"`
			Message string `json:"message"`
			Field   string `json:"field"`
		} `json:"causes"`
	} `json:"details"`
}

// AdjustRequestBody AdjustRequestBodyJSON takes the raw response body and a JSON request body,
// fixes any “FieldValueRequired” or “FieldValueNotSupported” errors, and
// returns the patched request body as JSON bytes.
func (c *FuzzCtx) AdjustRequestBody(requestBodyRef *openapi3.RequestBodyRef, reqBodyJSON, respBody []byte) ([]byte, string, error) {
	// 1. Parse Status from respBody
	var status Status
	if err := json.Unmarshal(respBody, &status); err != nil {
		return nil, "", fmt.Errorf("unmarshal status: %w", err)
	}

	causes := fmt.Sprintln(status.Details.Causes)

	// 2. Unmarshal the request body JSON into a generic map
	// FIXME: reqBody may not be in JSON format
	var reqMap map[string]any
	if err := json.Unmarshal(reqBodyJSON, &reqMap); err != nil {
		return nil, causes, fmt.Errorf("unmarshal request body: %w", err)
	}

	// Adjust Request Body Debugging
	//fmt.Println("==============================================")
	//fmt.Println(status.Details.Causes)
	//
	//fmt.Println("Press Enter to continue with request body patching...")
	//fmt.Scanln()

	// 3. Walk through each cause and patch accordingly
	for _, cause := range status.Details.Causes {
		var newVal any

		switch cause.Reason {
		case "FieldValueRequired", "FieldValueInvalid":
			// look for "valid values:" in the message
			if opts := parseOptions(cause.Message, "valid values:"); len(opts) > 0 {
				// TODO: this is a hack, currently we only pick random value
				newVal = opts[utils.R.IntN(len(opts))]
			} else {
				val, err := c.randValueByFieldPath(requestBodyRef, cause.Field)
				if err != nil {
					logrus.Error(err)
					continue
				}
				newVal = val
			}

		case "FieldValueNotSupported":
			// parse "supported values:"
			if opts := parseOptions(cause.Message, "supported values:"); len(opts) > 0 {
				// TODO: this is a hack, currently we only pick random value
				newVal = opts[utils.R.IntN(len(opts))]
			} else {
				// fallback
				newVal = 1
			}

		default:
			// TODO: implement procedures for other
			c.unexpectedLogger.Infof("unexpected cause reason: %q, message: %q", cause.Reason, cause.Message)
			continue
		}

		if err := setField(reqMap, cause.Field, newVal); err != nil {
			return nil, causes, fmt.Errorf("setField %q: %w", cause.Field, err)
		}
	}

	// 4. Marshal the patched map back to JSON
	patchedJSON, err := json.MarshalIndent(reqMap, "", "  ")
	if err != nil {
		return nil, causes, fmt.Errorf("marshal patched body: %w", err)
	}
	return patchedJSON, causes, nil
}

// parseOptions extracts all quoted strings after the given prefix.
// e.g. prefix="supported values:" from
//
//	`Unsupported value: "fixed": supported values: "A", "B", "C"`
//
// returns ["A","B","C"].
func parseOptions(message, prefix string) []string {
	idx := strings.Index(message, prefix)
	if idx < 0 {
		return nil
	}
	fragment := message[idx+len(prefix):]
	re := regexp.MustCompile(`"([^"]+)"`)
	var opts []string
	for _, m := range re.FindAllStringSubmatch(fragment, -1) {
		opts = append(opts, m[1])
	}
	return opts
}

// setField navigates the requestBody map according to a dotted path,
// handling array indices in the form "foo[0]". It sets the final key/index to value.
func setField(root map[string]any, path string, value any) error {
	parts := strings.Split(path, ".")
	var curr any = root

	for i, part := range parts {
		isLast := i == len(parts)-1

		// check for array index, e.g., "rules[0]"
		name, idx, hasIdx := parseIndex(part)

		switch container := curr.(type) {
		case map[string]any:
			if hasIdx {
				// get or create slice
				v, _ := container[name]
				slice, ok := v.([]any)
				if !ok {
					return fmt.Errorf("field %q is not a slice", name)
				}
				if idx < 0 || idx >= len(slice) {
					return fmt.Errorf("index %d out of range in %q", idx, name)
				}
				if isLast {
					slice[idx] = value
					return nil
				}
				curr = slice[idx]

			} else {
				if isLast {
					container[name] = value
					return nil
				}
				next, ok := container[name]
				if !ok {
					m := make(map[string]any)
					container[name] = m
					curr = m
				} else {
					curr = next
				}
			}

		default:
			return fmt.Errorf("unexpected type %T at %q", curr, part)
		}
	}

	return nil
}

// parseIndex checks if a part has "[i]" and returns (baseName, i, true).
// If no index, returns (part, 0, false).
func parseIndex(part string) (string, int, bool) {
	if !strings.Contains(part, "[") {
		return part, 0, false
	}
	name := part[:strings.Index(part, "[")]
	idxStr := part[strings.Index(part, "[")+1 : strings.Index(part, "]")]
	var idx int
	if _, err := fmt.Sscanf(idxStr, "%d", &idx); err != nil {
		return "", 0, false
	}
	return name, idx, true
}
