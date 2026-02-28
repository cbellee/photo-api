package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Condition represents a single predicate in an Azure blob-tag filter query.
type Condition struct {
	IsContainer bool   // true when the key is @container
	Key         string // tag key (or "@container")
	Value       string // expected value
}

var andSplitter = regexp.MustCompile(`(?i)\s+and\s+`)

// ParseTagQuery splits an Azure-style tag filter expression into conditions.
//
// Supported syntax (matches what the photo-api handlers produce):
//
//	@container='images' AND collection='trips' AND album='hong kong'
//	"tagKey"='value' and anotherKey='value'
func ParseTagQuery(query string) ([]Condition, error) {
	parts := andSplitter.Split(query, -1)

	var conditions []Condition
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		eqIdx := strings.Index(part, "=")
		if eqIdx < 0 {
			return nil, fmt.Errorf("invalid condition (no '='): %s", part)
		}

		key := strings.TrimSpace(part[:eqIdx])
		value := strings.TrimSpace(part[eqIdx+1:])

		// Strip optional double-quotes around key.
		key = strings.Trim(key, `"`)
		// Strip single-quotes around value.
		value = strings.Trim(value, "'")

		conditions = append(conditions, Condition{
			IsContainer: key == "@container",
			Key:         key,
			Value:       value,
		})
	}

	if len(conditions) == 0 {
		return nil, fmt.Errorf("empty query")
	}
	return conditions, nil
}

// BuildFilterSQL converts parsed conditions into a parameterised SQL query
// that returns (id, container, name) rows from the blobs table.
func BuildFilterSQL(conditions []Condition) (string, []interface{}) {
	var clauses []string
	var args []interface{}

	for _, c := range conditions {
		if c.IsContainer {
			clauses = append(clauses, "b.container = ?")
			args = append(args, c.Value)
		} else {
			clauses = append(clauses,
				"EXISTS (SELECT 1 FROM tags t WHERE t.blob_id = b.id AND t.key = ? AND t.value = ?)")
			args = append(args, c.Key, c.Value)
		}
	}

	sql := "SELECT b.id, b.container, b.name FROM blobs b"
	if len(clauses) > 0 {
		sql += " WHERE " + strings.Join(clauses, " AND ")
	}
	return sql, args
}
