package vault

// DeepMerge recursively merges two maps. Values from existing take precedence.
// Only keys present in incoming but absent in existing are added.
// For nested maps, the merge recurses so that sub-keys are also preserved.
func DeepMerge(existing, incoming map[string]any) map[string]any {
	if existing == nil {
		return copyMap(incoming)
	}
	if incoming == nil {
		return copyMap(existing)
	}

	result := copyMap(existing)

	for key, incomingVal := range incoming {
		existingVal, exists := result[key]
		if !exists {
			// Key only in incoming — add it
			result[key] = deepCopy(incomingVal)
			continue
		}

		// Both exist — recurse if both are maps, otherwise keep existing
		existingMap, existingIsMap := existingVal.(map[string]any)
		incomingMap, incomingIsMap := incomingVal.(map[string]any)

		if existingIsMap && incomingIsMap {
			result[key] = DeepMerge(existingMap, incomingMap)
		}
		// else: existing scalar/slice wins — do nothing
	}

	return result
}

// Equal reports whether two maps have the same structure and values.
func Equal(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		if !valuesEqual(av, bv) {
			return false
		}
	}
	return true
}

func valuesEqual(a, b any) bool {
	aMap, aIsMap := a.(map[string]any)
	bMap, bIsMap := b.(map[string]any)
	if aIsMap && bIsMap {
		return Equal(aMap, bMap)
	}

	aSlice, aIsSlice := a.([]any)
	bSlice, bIsSlice := b.([]any)
	if aIsSlice && bIsSlice {
		if len(aSlice) != len(bSlice) {
			return false
		}
		for i := range aSlice {
			if !valuesEqual(aSlice[i], bSlice[i]) {
				return false
			}
		}
		return true
	}

	return a == b
}

func copyMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = deepCopy(v)
	}
	return result
}

func deepCopy(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return copyMap(val)
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = deepCopy(item)
		}
		return result
	default:
		return v
	}
}
