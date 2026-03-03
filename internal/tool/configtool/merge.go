package configtool

import "gopkg.in/yaml.v3"

// mergeNodes performs a recursive merge of patch into base following
// JSON Merge Patch (RFC 7386) semantics adapted for YAML:
//
//   - Mapping + Mapping: recursive merge, patch keys override base.
//   - Sequence + Sequence: patch replaces base entirely.
//   - Scalar → anything: patch replaces base.
//   - null in patch: removes the key from base.
//   - New key in patch: appended to the end of the mapping.
//
// The function works on *yaml.Node to preserve ${VAR} references,
// comments, and key ordering in the original file.
func mergeNodes(base, patch *yaml.Node) *yaml.Node {
	// Unwrap document nodes.
	if base != nil && base.Kind == yaml.DocumentNode && len(base.Content) > 0 {
		base = base.Content[0]
	}
	if patch != nil && patch.Kind == yaml.DocumentNode && len(patch.Content) > 0 {
		patch = patch.Content[0]
	}

	if base == nil {
		return patch
	}
	if patch == nil {
		return base
	}

	// Only merge mapping+mapping recursively; everything else: patch wins.
	if base.Kind == yaml.MappingNode && patch.Kind == yaml.MappingNode {
		mergeMappings(base, patch)
		return base
	}

	// Patch replaces base entirely (scalar, sequence, or type mismatch).
	return patch
}

// mergeMappings merges patch mapping keys into base mapping in place.
func mergeMappings(base, patch *yaml.Node) {
	for i := 0; i < len(patch.Content)-1; i += 2 {
		patchKey := patch.Content[i]
		patchVal := patch.Content[i+1]

		idx := findMappingKey(base, patchKey.Value)

		// null value in patch means "delete this key".
		if patchVal.Tag == "!!null" || (patchVal.Kind == yaml.ScalarNode && patchVal.Value == "null" && patchVal.Tag == "") {
			if idx >= 0 {
				// Remove key+value pair from base.
				base.Content = append(base.Content[:idx], base.Content[idx+2:]...)
			}
			continue
		}

		if idx >= 0 {
			// Key exists in base — merge recursively or replace.
			base.Content[idx+1] = mergeNodes(base.Content[idx+1], patchVal)
		} else {
			// New key — append to end.
			base.Content = append(base.Content, patchKey, patchVal)
		}
	}
}

// findMappingKey returns the index of the key node with the given value
// in a mapping node's Content slice. Returns -1 if not found.
func findMappingKey(mapping *yaml.Node, key string) int {
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			return i
		}
	}
	return -1
}
