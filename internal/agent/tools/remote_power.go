
// powerOffline answers a call made while the computer is offline after a
// power request: status succeeds and explains, other actions fail with
// the reason instead of a generic connection fault.
func (t *remoteSupportTool) powerOffline(action string, dev supportDevice, r powerRequest) supportResult {
	t.power.advance(r.DeviceID, powerDisconnected)
	r.State = powerDisconnected
	res := supportResult{Action: action, Device: dev, Effect: support.EffectNone, Verified: "power_" + r.State, Result: map[string]any{"state": "offline_after_" + r.Kind, "power_request": r}}
	res.Note = powerNote(r)
	if action == "status" {
		res.OK = true
		return res
	}
	res.Error = support.Errorf(support.CodeRemoteOffline, "the computer is offline after it was asked to %s at %s; the cause is not confirmed and nothing new was sent", r.Kind, r.SentAt)
	res.Error.RemoteDeviceID = r.DeviceID
	res.Error.NextStep = "Report that the computer is unreachable after the power request; do not infer the cause or resend it. Continue only after it is back and status shows a session."
	return res
}
