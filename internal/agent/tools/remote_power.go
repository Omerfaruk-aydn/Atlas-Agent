		if slices.Contains(args, "poweroff") || slices.Contains(args, "halt") {
			return powerShutdown, true
		}
		if slices.Contains(args, "reboot") {
			return powerRestart, true
		}
	case "powershell", "pwsh":
		if i := slices.IndexFunc(args, func(a string) bool { return a == "-command" || a == "-c" }); i >= 0 && i+1 < len(args) {
			script := strings.Join(argv[i+2:], " ")
			// Only a simple literal invocation is classified. Quoted output,
			// pipelines and conditional scripts keep normal unknown semantics.
			if strings.ContainsAny(script, "'\";|&$`{}()") {
				return "", false
			}
			return classifyPowerCommand(strings.Fields(script))
		}
	case "cmd":
		if i := slices.IndexFunc(args, func(a string) bool { return a == "/c" || a == "/k" }); i >= 0 && i+1 < len(args) {
			return classifyPowerCommand(argv[i+2:])
		}
	case "restart-computer":
		return powerRestart, true
	case "stop-computer":
		return powerShutdown, true
	}
	return "", false
}

// powerOutcome turns the result of a sent power request into its expected
// state. A clean exit proves acceptance. A dropped connection leaves the
// actual machine state unknown, without authorizing a duplicate request.
func (t *remoteSupportTool) powerOutcome(kind string, res supportResult, e *support.Error, opID string) (supportResult, bool) {
	req := powerRequest{Kind: kind, State: powerSent, DeviceID: res.Device.RemoteDeviceID, SessionID: res.Device.SessionID, OperationID: opID}
	switch {
	case e == nil && res.OK:
		req.State = powerAccepted
	case e != nil && (e.Code == support.CodeOutcomeUnknown || e.Code == support.CodeConnectionLost || e.Code == support.CodeRemoteOffline) && e.InputSent:
		req.State = powerDisconnected
	default:
		return res, false
	}
	t.power.set(req)
	res.InputSent = true
	if req.State == powerAccepted {
		res.OK, res.Error, res.Checkpoint = true, nil, nil
		res.Effect = support.EffectApplied
	} else {
		res.OK, res.Error = false, e
		res.Effect = support.EffectUnknown
	}
	res.Verified = "power_" + req.State
	res.Result = map[string]any{"power_request": req}
	res.Note = powerNote(req)
	return res, true
}

func powerNote(r powerRequest) string {
	what := "shut down"
	after := "It stays offline until someone turns it on and reconnects with /support."
	if r.Kind == powerRestart {
		what = "restart"
		after = "It should come back after the restart; call status later. A new support session may be needed."
	}
	switch r.State {
	case powerAccepted:
		return "The remote computer accepted the request to " + what + ". Losing the connection next is expected, not a failure. Do not send it again and do not ask the user to confirm again. " + after
	case powerDisconnected:
		return "The connection dropped after the request to " + what + " was sent. This disconnect is expected for a power request, but the command outcome is still unknown; a network failure can look the same. Do not repeat the command or ask for the same permission again. " + after
	}
	return "A request to " + what + " was sent. Do not send it again. " + after
}

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
