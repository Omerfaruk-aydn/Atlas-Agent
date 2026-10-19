package codemap

import (
	"fmt"
	"sort"
	"strings"
)

// Limits for the parts of an answer about documents and assets.
const (
	maxOverviewDocs   = 8
	maxListedProblems = 8
	maxListedAssets   = 5
)

