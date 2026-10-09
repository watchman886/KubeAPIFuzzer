package run

import (
	"k-fuzz/utils"

	"github.com/getkin/kin-openapi/openapi3"
)

func (c *FuzzCtx) MutateParams(params map[string]string, paramSchemaMap map[string]*openapi3.ParameterRef) map[string]string {
	// copy params to mutatedParams
	mutatedParams := make(map[string]string, len(params))
	for key, value := range params {
		mutatedParams[key] = value
	}

	// perform a random number of mutations (add / delete / change)
	for n := 1 + utils.R.IntN(32); n > 0; n-- {
		switch utils.R.IntN(3) {
		case 0:
			// remove a random existing param
			if len(mutatedParams) == 0 {
				continue
			}
			keys := make([]string, 0, len(mutatedParams))
			for k := range mutatedParams {
				keys = append(keys, k)
			}
			k := keys[utils.R.IntN(len(keys))]
			delete(mutatedParams, k)
		case 1:
			// add a parameter that exists in schema but not yet in mutatedParams
			candidates := make([]string, 0, len(paramSchemaMap))
			for k := range paramSchemaMap {
				if _, exists := mutatedParams[k]; !exists {
					candidates = append(candidates, k)
				}
			}
			if len(candidates) == 0 {
				continue
			}
			addKey := candidates[utils.R.IntN(len(candidates))]
			if pref := paramSchemaMap[addKey]; pref != nil {
				mutatedParams[addKey] = c.valueProvider.FillParameter(pref)
			} else {
				// fallback to an empty string if schema ref missing
				mutatedParams[addKey] = utils.StringRefMissingFallback
			}
		case 2:
			// change an existing parameter's value
			if len(mutatedParams) == 0 {
				continue
			}
			keys := make([]string, 0, len(mutatedParams))
			for k := range mutatedParams {
				keys = append(keys, k)
			}
			chKey := keys[utils.R.IntN(len(keys))]
			if pref, ok := paramSchemaMap[chKey]; ok && pref != nil {
				mutatedParams[chKey] = c.valueProvider.FillParameter(pref)
			} else {
				// fallback: attempt to produce a random string value
				mutatedParams[chKey] = c.valueProvider.FillString([]string{}, 0, nil)
			}
		}
	}

	return mutatedParams
}
