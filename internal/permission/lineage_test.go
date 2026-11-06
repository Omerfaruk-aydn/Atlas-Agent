package permission

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLineageRootFollowsTheChain(t *testing.T) {
	t.Parallel()
	var l lineage
	l.Link("grandchild", "child")
	l.Link("child", "root")
	require.Equal(t, "root", l.Root("grandchild"))
	require.Equal(t, "root", l.Root("child"))
	require.Equal(t, "root", l.Root("root"))
	require.Equal(t, "other", l.Root("other"))

	l.Forget("child")
	require.Equal(t, "child", l.Root("grandchild"))
}

func TestLineageSurvivesALoop(t *testing.T) {
	t.Parallel()
	var l lineage
	l.Link("a", "b")
	l.Link("b", "a")
	require.NotEmpty(t, l.Root("a"), "a corrupt loop ends instead of spinning")
	l.Link("self", "self")
	require.Equal(t, "self", l.Root("self"))
}

func TestASessionGrantReachesTheSubAgentsOfThatSession(t *testing.T) {
	t.Parallel()
	svc := NewPermissionService(t.TempDir(), false, nil).(*permissionService)
	svc.LinkSession("child", "parent")

	require.NoError(t, svc.GrantScoped("parent", "bash", "execute", svc.workingDir, GrantSession))
	_, ok := svc.sessionPermissions.Get(PermissionKey{SessionID: svc.lineage.Root("child"), ToolName: "bash", Action: "execute", Path: svc.workingDir})
	require.True(t, ok, "the child session resolves to the parent's grant")

	svc.UnlinkSession("child")
	require.Equal(t, "child", svc.lineage.Root("child"))
}
