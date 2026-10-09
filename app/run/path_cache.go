package run

import (
	"encoding/json"
	"strings"

	"k-fuzz/utils"
)

type PathValueCache struct {
	namesByKey map[string][]string
}

func NewPathValueCache() *PathValueCache {
	return &PathValueCache{
		namesByKey: make(map[string][]string),
	}
}

func (c *PathValueCache) Observe(templatePath string, requestKind string, body []byte) {
	if c == nil || len(body) == 0 {
		return
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return
	}

	collectionKey := collectionSegmentFromTemplate(templatePath)
	if items, ok := payload["items"].([]any); ok {
		for _, item := range items {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			itemKind, _ := obj["kind"].(string)
			c.observeObject(collectionKey, firstNonEmpty(itemKind, requestKind), obj)
		}
		return
	}

	responseKind, _ := payload["kind"].(string)
	c.observeObject(collectionKey, firstNonEmpty(responseKind, requestKind), payload)
}

func (c *PathValueCache) Lookup(templatePath string, paramName string, requestKind string) (string, bool) {
	if c == nil {
		return "", false
	}

	for _, key := range cacheLookupKeys(templatePath, paramName, requestKind) {
		values := c.namesByKey[key]
		if len(values) == 0 {
			continue
		}
		return values[utils.R.IntN(len(values))], true
	}

	return "", false
}

func (c *PathValueCache) add(key string, value string) {
	key = normalizeCacheKey(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return
	}

	for _, existing := range c.namesByKey[key] {
		if existing == value {
			return
		}
	}

	c.namesByKey[key] = append(c.namesByKey[key], value)
}

func (c *PathValueCache) observeObject(collectionKey string, requestKind string, obj map[string]any) {
	name := objectName(obj)
	if name != "" {
		for _, key := range cacheKeysForResource(collectionKey, requestKind) {
			c.add(key, name)
		}
	}

	for _, containerName := range objectContainerNames(obj) {
		for _, key := range cacheKeysForParameter(collectionKey, "container", requestKind) {
			c.add(key, containerName)
		}
	}
}

func cacheLookupKeys(templatePath string, paramName string, requestKind string) []string {
	keys := make([]string, 0, 6)
	seen := make(map[string]struct{}, 6)
	add := func(key string) {
		key = normalizeCacheKey(key)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}

	contextKey := placeholderContextKey(templatePath, paramName)
	add(contextKey)
	add(singularizeResourceKey(contextKey))

	collectionKey := collectionSegmentFromTemplate(templatePath)
	for _, key := range cacheKeysForParameter(collectionKey, paramName, requestKind) {
		add(key)
	}

	switch normalizedName := normalizeCacheKey(paramName); normalizedName {
	case "", "name", "namespace", "path":
	default:
		add(normalizedName)
		add(pluralizeResourceKey(normalizedName))
	}

	add(requestKind)
	add(pluralizeResourceKey(requestKind))

	return keys
}

func cacheKeysForParameter(collectionKey string, paramName string, requestKind string) []string {
	keys := make([]string, 0, 6)
	seen := make(map[string]struct{}, 6)
	add := func(key string) {
		key = normalizeCacheKey(key)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}

	normalizedParam := normalizeCacheKey(paramName)
	if normalizedParam == "" {
		return nil
	}

	for _, resourceKey := range cacheKeysForResource(collectionKey, requestKind) {
		add(resourceKey + "-" + normalizedParam)
	}
	add(normalizedParam)

	return keys
}

func cacheKeysForResource(collectionKey string, requestKind string) []string {
	keys := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)
	add := func(key string) {
		key = normalizeCacheKey(key)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}

	add(collectionKey)
	add(singularizeResourceKey(collectionKey))
	add(requestKind)
	add(pluralizeResourceKey(requestKind))

	return keys
}

func placeholderContextKey(templatePath string, paramName string) string {
	segments := splitPathSegments(templatePath)
	target := "{" + paramName + "}"
	for i, segment := range segments {
		if segment != target {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if isPlaceholderSegment(segments[j]) {
				continue
			}
			return segments[j]
		}
	}

	return ""
}

func collectionSegmentFromTemplate(templatePath string) string {
	segments := splitPathSegments(templatePath)
	for i, segment := range segments {
		if !isPlaceholderSegment(segment) || segment == "{namespace}" {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if isPlaceholderSegment(segments[j]) {
				continue
			}
			return segments[j]
		}
	}

	for i := len(segments) - 1; i >= 0; i-- {
		if isPlaceholderSegment(segments[i]) {
			continue
		}
		return segments[i]
	}

	return ""
}

func splitPathSegments(templatePath string) []string {
	trimmed := strings.Trim(templatePath, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func isPlaceholderSegment(segment string) bool {
	return strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}")
}

func normalizeCacheKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

func singularizeResourceKey(key string) string {
	key = normalizeCacheKey(key)
	switch {
	case strings.HasSuffix(key, "ies") && len(key) > 3:
		return strings.TrimSuffix(key, "ies") + "y"
	case strings.HasSuffix(key, "sses") && len(key) > 4:
		return strings.TrimSuffix(key, "es")
	case strings.HasSuffix(key, "ses") && len(key) > 3:
		return strings.TrimSuffix(key, "es")
	case strings.HasSuffix(key, "s") && len(key) > 1:
		return strings.TrimSuffix(key, "s")
	default:
		return key
	}
}

func pluralizeResourceKey(key string) string {
	key = singularizeResourceKey(key)
	switch {
	case key == "":
		return ""
	case strings.HasSuffix(key, "y") && len(key) > 1:
		return strings.TrimSuffix(key, "y") + "ies"
	case strings.HasSuffix(key, "s"):
		return key + "es"
	default:
		return key + "s"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func objectName(obj map[string]any) string {
	metadata, ok := obj["metadata"].(map[string]any)
	if !ok {
		return ""
	}

	name, _ := metadata["name"].(string)
	return strings.TrimSpace(name)
}

func objectContainerNames(obj map[string]any) []string {
	spec, ok := obj["spec"].(map[string]any)
	if !ok {
		return nil
	}

	names := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)
	addNames := func(field string) {
		items, ok := spec[field].([]any)
		if !ok {
			return
		}
		for _, item := range items {
			container, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := container["name"].(string)
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}

	addNames("containers")
	addNames("initContainers")
	addNames("ephemeralContainers")

	return names
}
