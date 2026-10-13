package support

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoteContextNamesTheLiveTarget(t *testing.T) {
	t.Parallel()
	r := &Remote{SessionInfo: SessionInfo{RemoteName: "Omerfaruk", RemoteDeviceID: "node-e56a", SessionID: "ss-d70f", Generation: 3, OS: "windows", Capabilities: []Capability{CapScreenView, CapInput}}}
	got := remoteContext(ControllerStatus{State: StateConnected, Remote: r})
	require.Contains(t, got, "remote_device_id=node-e56a session_id=ss-d70f generation=3")
	require.Contains(t, got, "granted=screen_view,input")
	require.Contains(t, got, "never use them for the remote one")

	reconnecting := remoteContext(ControllerStatus{State: StateReconnecting, Remote: r})
	require.Contains(t, reconnecting, "do not switch to this computer")

	for _, st := range []State{StateIdle, StateDisconnected, StateExpired, StateRevoked, StateFailed} {
		require.Empty(t, remoteContext(ControllerStatus{State: st, Remote: r}), st)
	}
	require.Empty(t, remoteContext(ControllerStatus{State: StateConnected}))
	var nilCtl *Controller
	require.Empty(t, nilCtl.RemoteContext())
	name, id := nilCtl.ActiveRemote()
	require.Empty(t, name+id)
}
