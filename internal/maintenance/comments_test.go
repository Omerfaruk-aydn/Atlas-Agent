package maintenance

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommentTasksIgnoresStringLiteralsAndDeduplicates(t *testing.T) {
	t.Parallel()
	tasks, err := CommentTasks(t.Context(), "fixture.go", []byte("package fixture\nvar text = `// ATLAS: never execute`\n// ATLAS: add boundary tests\n// ATLAS: add boundary tests\n"))
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, "add boundary tests", tasks[0].Prompt)
	require.Equal(t, 3, tasks[0].Line)
}
