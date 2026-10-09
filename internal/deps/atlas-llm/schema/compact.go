package schema

import (
	"encoding/json"
	"slices"
	"strings"
)

// minRepeatedDocBytes is the amount of nested documentation a repeated
// object must carry before its copy is replaced by a pointer note. Smaller
// shapes stay fully documented because the note would save almost nothing.
const minRepeatedDocBytes = 160

// CompactRepeated keeps nested field documentation on the first occurrence of
// each repeated object shape. Later copies keep every type, enum, bound and
// required list, so validation and the model-visible contract stay exact; only
// their nested descriptions are replaced by a note naming the documented
// occurrence. No $ref is introduced, because several providers reject or
// mishandle references in tool schemas.
//
// Traversal is breadth first with sorted property names, so the documented
// occurrence is always the shallowest one and the result is deterministic.
func CompactRepeated(root *Schema) {
	if root == nil {
		return
	}
	seen := map[string]string{}
	queue := childItems(root, "")
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		node := current.node
		if isCompound(node) {
			key := shapeKey(node)
			if first, ok := seen[key]; ok {
				if nestedDocBytes(node) >= minRepeatedDocBytes {
					stripNestedDescriptions(node)
					node.Description = appendNote(node.Description, "Same fields and constraints as "+first+".")
				}
				continue
			}
			seen[key] = current.path
		}
		queue = append(queue, childItems(node, current.path)...)
	}
}

type schemaItem struct {
	node *Schema
	path string
}

func childItems(node *Schema, path string) []schemaItem {
	var out []schemaItem
	names := make([]string, 0, len(node.Properties))
	for name := range node.Properties {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		child := node.Properties[name]
		if child == nil {
			continue
		}
		out = append(out, schemaItem{child, joinPath(path, name)})
	}
	if node.Items != nil {
		out = append(out, schemaItem{node.Items, path + "[]"})
	}
	return out
}

func joinPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "." + name
}

func isCompound(node *Schema) bool {
	return len(node.Properties) > 0 || (node.Items != nil && isCompound(node.Items))
}

// shapeKey identifies a subtree independently of the description its parent
// field gives it, which is specific to that field.
func shapeKey(node *Schema) string {
	clone := *node
	clone.Description = ""
	data, err := json.Marshal(clone)
	if err != nil {
		return ""
	}
	return string(data)
}

func nestedDocBytes(node *Schema) int {
	total := 0
	for _, child := range node.Properties {
		if child != nil {
			total += len(child.Description) + nestedDocBytes(child)
		}
	}
	if node.Items != nil {
		total += len(node.Items.Description) + nestedDocBytes(node.Items)
	}
	return total
}

func stripNestedDescriptions(node *Schema) {
	for _, child := range node.Properties {
		if child != nil {
			child.Description = ""
			stripNestedDescriptions(child)
		}
	}
	if node.Items != nil {
		node.Items.Description = ""
		stripNestedDescriptions(node.Items)
	}
}

func appendNote(description, note string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return note
	}
	return description + " " + note
}

// Walk visits every subschema below root in a deterministic order. The path
// uses the same dotted notation as CompactRepeated's notes.
func Walk(root *Schema, visit func(path string, node *Schema)) {
	if root == nil {
		return
	}
	queue := childItems(root, "")
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		visit(current.path, current.node)
		queue = append(queue, childItems(current.node, current.path)...)
	}
}
