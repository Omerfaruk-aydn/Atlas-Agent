package support

import (
	"fmt"
	"strings"
)

// RemoteContext describes the computer this one is supporting, for the
// model. It is rebuilt from the live controller on every model step, so it
// survives summarization, resumed tasks and permission pauses, and it
// changes as soon as the device, session or generation does. It is empty
// when no other computer is connected.
func (c *Controller) RemoteContext() string {
	if c == nil {
		return ""
	}
	return remoteContext(c.Status())
}

func remoteContext(st ControllerStatus) string {
	r := st.Remote
	if r == nil {
		return ""
	}
	switch st.State {
	case StateConnected, StatePaused, StateReconnecting:
	default:
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "This computer is supporting %q through /support.\n", r.RemoteName)
	fmt.Fprintf(&b, "remote_device_id=%s session_id=%s generation=%d os=%s state=%s\n", r.RemoteDeviceID, r.SessionID, r.Generation, r.OS, st.State)
	if len(r.Capabilities) > 0 {
		caps := make([]string, 0, len(r.Capabilities))
		for _, c := range r.Capabilities {
			caps = append(caps, string(c))
		}
		fmt.Fprintf(&b, "granted=%s\n", strings.Join(caps, ","))
	}
	b.WriteString("Requests about opening apps, the browser, the screen, files or shutting down mean that computer unless the user explicitly says this computer. ")
	b.WriteString("Act there only with remote_support (tool=browser/computer/tool_pipeline for built-ins) and pass this device_id and session_id. ")
	b.WriteString("The local browser, computer and bash tools act on THIS computer; never use them for the remote one. ")
	b.WriteString("Showing the user a remote screenshot you already have is fine.")
	if st.State == StateReconnecting {
		b.WriteString(" The connection is being restored: do not switch to this computer meanwhile.")
	}
	if st.State == StatePaused {
		b.WriteString(" The owner paused the session; wait for them.")
	}
	return b.String()
}

// ActiveRemote returns the supported computer's name and session id while
// a session is live (connected, paused or reconnecting), else empty
// strings.
func (c *Controller) ActiveRemote() (name, sessionID string) {
	if c == nil {
		return "", ""
	}
	st := c.Status()
	if st.Remote == nil {
		return "", ""
	}
	switch st.State {
	case StateConnected, StatePaused, StateReconnecting:
		return st.Remote.RemoteName, st.Remote.SessionID
	}
	return "", ""
}
