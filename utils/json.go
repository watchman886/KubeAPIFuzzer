package utils

func PruneNil(x any) any {
	switch v := x.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, val := range v {
			pr := PruneNil(val)
			if pr != nil {
				out[key] = pr
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out

	case []any:
		var outArr []any
		for _, elem := range v {
			pr := PruneNil(elem)
			if pr != nil {
				outArr = append(outArr, pr)
			}
		}
		if len(outArr) == 0 {
			return nil
		}
		return outArr

	default:
		if x == nil {
			return nil
		}
		return x
	}
}

// CollectLeafPaths walks through a JSON-like data structure (made up of map[string]any and []any)
// and returns every path from the root to each leaf node. Each path is represented as a []any,
// where elements are either string (map key) or int (slice index).
func CollectLeafPaths(root any) (paths [][]any) {
	// recurse is the actual recursive helper:
	//   - node is the current value
	//   - path is the sequence of keys/indices taken so far
	var recurse func(node any, path []any)
	recurse = func(node any, path []any) {
		switch n := node.(type) {
		case map[string]any:
			// for each key in the map, append the key to the path and recurse
			for key, val := range n {
				recurse(val, append(path, key))
			}
		case []any:
			// for each index in the slice, append the index to the path and recurse
			for idx, val := range n {
				recurse(val, append(path, idx))
			}
		default:
			// we've reached a leaf (neither map nor slice)
			// record the full path leading to this leaf
			if len(path) > 0 {
				// make a copy of path so future appends don't mutate stored paths
				p := make([]any, len(path))
				copy(p, path)
				paths = append(paths, p)
			}
		}
	}

	recurse(root, nil)

	return paths
}

func GetLeafParent(reqMap map[string]any, choice []any) (parent any) {
	parent = reqMap
	for _, step := range choice[:len(choice)-1] {
		switch key := step.(type) {
		case string:
			parent = parent.(map[string]any)[key]
		case int:
			parent = parent.([]any)[key]
		}
	}
	return parent
}
