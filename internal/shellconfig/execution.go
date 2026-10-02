package shellconfig

import (
	"context"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
)

func optionExecution(ctx context.Context, options map[string]any, args []string, stderr io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(args) < 3 || len(args) > 4 {
		return usage(stderr, "usage: option execution <key> [value]")
	}
	key, value := args[2], ""
	if len(args) == 4 {
		value = args[3]
	}
	execution := childMap(options, "execution")
	if key == "reset" && value == "environment-key" {
		execution["environment_keys"] = []any{}
		return nil
	}
	if key == "read-only" {
		if value == "" {
			value = "true"
		}
		parsed, err := parseBool(value)
		if err != nil {
			return usage(stderr, "execution read-only requires true/false")
		}
		execution["read_only"] = parsed
		return nil
	}
	if value == "" {
		return usage(stderr, "execution option requires a value")
	}
	switch key {
	case "mode":
		if value != "legacy" && value != "container-required" {
			return usage(stderr, "unsupported execution mode")
		}
		execution["mode"] = value
	case "runtime-path", "image":
		field := "runtime_path"
		if key == "image" {
			field = "image"
		}
		execution[field] = value
	case "network":
		if value != "none" && value != "unrestricted" {
			return usage(stderr, "unsupported execution network policy")
		}
		execution["network"] = value
	case "environment-key":
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(value) {
			return usage(stderr, "invalid execution environment key")
		}
		list := appendArr(execution, "environment_keys", value)
		if len(list) > 64 {
			return usage(stderr, "execution environment allow-list exceeds limit")
		}
		execution["environment_keys"] = list
	case "cpus":
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 || parsed > 64 {
			return usage(stderr, "execution cpus must be between 0 and 64")
		}
		execution["cpus"] = parsed
	case "memory-bytes", "max-processes", "timeout-ms":
		parsed, err := strconv.ParseInt(value, 10, 64)
		maximum, field := int64(64*1024*1024*1024), "memory_bytes"
		if key == "max-processes" {
			maximum, field = 4096, "max_processes"
		}
		if key == "timeout-ms" {
			maximum, field = 600000, "timeout_ms"
		}
		if err != nil || parsed < 0 || parsed > maximum {
			return usage(stderr, "execution resource value exceeds supported bounds")
		}
		execution[field] = parsed
	default:
		return usage(stderr, fmt.Sprintf("unknown execution option %q", key))
	}
	return nil
}
