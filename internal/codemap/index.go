package codemap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Limits keep the map bounded on any project.
const (
	maxFiles     = 20000
	maxFileBytes = 1 << 20
)

// generatedDirs are skipped when a folder without git is walked; git's own
// ignore rules cover them otherwise.
var generatedDirs = map[string]bool{
	".git": true, ".atlas": true, ".atlas-env": true, ".venv": true, "venv": true, "env": true,
	"node_modules": true, "bower_components": true, "vendor": true, "dist": true, "build": true, "out": true, "target": true,
	"coverage": true, "test-results": true, "playwright-report": true, "bin": true, "obj": true,
	".next": true, ".nuxt": true, ".svelte-kit": true, ".cache": true, ".turbo": true, ".parcel-cache": true,
	"__pycache__": true, ".pytest_cache": true, ".mypy_cache": true, ".tox": true, ".gradle": true, ".idea": true, ".vscode": true,
	"Pods": true, "DerivedData": true, ".dart_tool": true, "_build": true, "deps": true, "zig-cache": true, "zig-out": true,
}

// cacheEntry is a file's extraction and the stat it was read at.
type cacheEntry struct {
	Size    int64 `json:"size"`
	ModTime int64 `json:"mod_time"`
	File    File  `json:"file"`
}

// cacheVersion changes when extraction changes, so old caches are not
// reused.
const cacheVersion = 4

