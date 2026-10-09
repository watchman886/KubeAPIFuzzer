package collect_errors

import (
	"encoding/json"
	"fmt"
)

var prefixSet = map[string]struct{}{
	"no kind":                         {},
	"resourceVersion: Invalid value:": {},
	"Internal error occurred: failed to allocate a serviceIP:":            {},
	"json: cannot unmarshal":                                              {},
	"couldn't get version/kind; json parse error: json: cannot unmarshal": {},
	"no preferred addresses found; known addresses:":                      {},
}

func getHitPrefix(message string) (hitPrefix string) {
	hitPrefix = ""
	for prefix := range prefixSet {
		if len(message) >= len(prefix) &&
			message[:len(prefix)] == prefix {
			hitPrefix = prefix
			break
		}
	}
	return hitPrefix
}

// convertV2ToOutput parse resp message in v2 to string
// note: in v1, Req.Data.BodyBytes type is "any", and when parse v2 to v1, BodyBytes will be kept []byte and will be print as base64
func convertV2ToOutput(respV2 ErrorResponseV2) (ErrorResponseV1, error) {
	var bodyMessage BodyMessageV2
	err := json.Unmarshal(respV2.Resp.Body, &bodyMessage)

	if err != nil {
		return ErrorResponseV1{}, fmt.Errorf("failed to unmarshal response body: %w", err)
	}

	return ErrorResponseV1{
		Req: respV2.Req,
		Resp: struct {
			StatusCode int `json:"StatusCode"`
			Body       struct {
				Message string `json:"message"`
			} `json:"Body"`
		}{
			StatusCode: respV2.Resp.StatusCode,
			Body:       bodyMessage,
		},
	}, nil
}
