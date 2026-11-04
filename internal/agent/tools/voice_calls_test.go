package tools

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
	"github.com/stretchr/testify/require"
)

// permissionedTool asks the real permission service before acting, the way
// MCP tools do.
func permissionedTool(t *testing.T, name string, svc permission.Service, ran *atomic.Int32) fantasy.AgentTool {
	t.Helper()
	return fantasy.NewAgentTool(name, "test tool", func(ctx context.Context, _ struct{}, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		ok, err := svc.Request(ctx, permission.CreatePermissionRequest{SessionID: "s", ToolCallID: call.ID, ToolName: name, Action: "run"})
		if err != nil {
			return fantasy.NewTextErrorResponse("permission: " + err.Error()), nil
		}
		if !ok {
			return fantasy.NewTextErrorResponse("denied"), nil
		}
		ran.Add(1)
		return fantasy.NewTextResponse("done"), nil
	})
}

func TestVoicePolicyNeverBypassesPermissions(t *testing.T) {
	t.Parallel()
	svc := permission.NewPermissionService(t.TempDir(), false, nil)
	policy := voice.NewToolPolicy(voice.ToolPolicyConfig{}, func(n string) bool { return n == "view" })
	var ran atomic.Int32
	calendar := WithVoicePolicy(permissionedTool(t, "mcp_calendar_create_event", svc, &ran))
	bash := WithVoicePolicy(permissionedTool(t, "bash", svc, &ran))
	call := func(name, id string) fantasy.ToolCall { return fantasy.ToolCall{ID: id, Name: name, Input: "{}"} }

	answer := atomic.Bool{}
	asked := atomic.Int32{}
	turn := &voice.Turn{Policy: policy, Confirm: func(context.Context, voice.ConfirmRequest) (bool, error) {
		asked.Add(1)
		return answer.Load(), nil
	}}
	// A voice turn runs headless: nobody can answer a terminal prompt.
	ctx := permission.WithoutPrompts(voice.WithTurn(context.Background(), turn))

	resp, err := calendar.Run(ctx, call("mcp_calendar_create_event", "c1"))
	require.NoError(t, err)
	require.True(t, resp.IsError, "declined confirmation runs nothing")
	require.Contains(t, resp.Content, "did not confirm")
	require.Zero(t, ran.Load())

	answer.Store(true)
	resp, err = calendar.Run(ctx, call("mcp_calendar_create_event", "c2"))
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, int32(1), ran.Load(), "a spoken yes pre-approves exactly that call")

	resp, err = bash.Run(ctx, call("bash", "c3"))
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "cannot be used during a phone call")
	require.Equal(t, int32(2), asked.Load(), "blocked tools are never offered for confirmation")
	require.Equal(t, int32(1), ran.Load())

	// Outside a voice turn nothing changes: a headless run without a
	// grant is still refused by the permission service.
	resp, err = calendar.Run(permission.WithoutPrompts(context.Background()), call("mcp_calendar_create_event", "c4"))
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "permission")
	require.Equal(t, int32(1), ran.Load())

	// Plan mode stays read-only even after a spoken yes.
	svc.SetMode(permission.ModePlan)
	resp, err = calendar.Run(ctx, call("mcp_calendar_create_event", "c5"))
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Equal(t, int32(1), ran.Load())
}

type fakeVoiceCalls struct {
	req    voice.OwnerCallRequest
	result voice.OwnerCallResult
	err    error
}

func (f *fakeVoiceCalls) CallOwner(_ context.Context, req voice.OwnerCallRequest) (voice.OwnerCallResult, error) {
	f.req = req
	return f.result, f.err
}

func (f *fakeVoiceCalls) Call(context.Context, string) (voice.CallRecord, error) {
	return voice.CallRecord{ID: "c1", State: voice.StateEnded}, nil
}

func (f *fakeVoiceCalls) Calls(context.Context, int) ([]voice.CallRecord, error) { return nil, nil }

func (f *fakeVoiceCalls) Transcript(context.Context, string) (voice.Transcript, error) {
	return voice.Transcript{CallID: "c1", Stored: false}, nil
}

func (f *fakeVoiceCalls) Status(context.Context) (voice.StatusView, error) {
	return voice.StatusView{Sites: []voice.SiteState{{Name: "shop", Health: voice.HealthDown}}}, nil
}

func (f *fakeVoiceCalls) SetNotifyOnRecovery(_ context.Context, site string, on bool) (voice.SiteState, error) {
	return voice.SiteState{Name: site, NotifyOnRecovery: on}, nil
}

type decidingPermissions struct {
	mockPermissionService
	allow bool
	asked []permission.CreatePermissionRequest
}

func (d *decidingPermissions) Request(_ context.Context, req permission.CreatePermissionRequest) (bool, error) {
	d.asked = append(d.asked, req)
	return d.allow, nil
}

func voiceTool(t *testing.T, ts []fantasy.AgentTool, name string) fantasy.AgentTool {
	t.Helper()
	for _, tool := range ts {
		if tool.Info().Name == name {
			return tool
		}
	}
	t.Fatalf("tool %s missing", name)
	return nil
}

func TestCallOwnerTool(t *testing.T) {
	vc := &fakeVoiceCalls{result: voice.OwnerCallResult{Result: voice.ResultNoAnswer, Fallback: []string{"sms"}}}
	perms := &decidingPermissions{mockPermissionService: mockPermissionService{Broker: pubsub.NewBroker[permission.PermissionRequest]()}, allow: true}
	ts := NewVoiceCallTools(func() (VoiceCalls, error) { return vc, nil }, perms)
	callOwner := voiceTool(t, ts, CallOwnerToolName)
	run := func(input string) fantasy.ToolResponse {
		resp, err := callOwner.Run(context.Background(), fantasy.ToolCall{ID: "t1", Name: CallOwnerToolName, Input: input})
		require.NoError(t, err)
		return resp
	}

	t.Setenv("ATLAS_AGENT_ROUTINE", "morning-call")
	resp := run(`{"reason":"Günün sözü hazır."}`)
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "written message went out on sms")
	require.Contains(t, resp.Content, "Do not call again")
	require.Regexp(t, `^routine:morning-call:\d{4}-\d{2}-\d{2}$`, vc.req.EventID, "routine runs get a per-day idempotency key")
	require.Len(t, perms.asked, 1)
	require.Equal(t, "call", perms.asked[0].Action)

	b, _ := json.Marshal(CallOwnerParams{Reason: "x", EventID: "monitor:shop:1:down"})
	run(string(b))
	require.Equal(t, "monitor:shop:1:down", vc.req.EventID)

	resp = run(`{"reason":"  "}`)
	require.True(t, resp.IsError)

	perms.allow = false
	vc.req = voice.OwnerCallRequest{}
	resp = run(`{"reason":"izinsiz"}`)
	require.True(t, resp.IsError)
	require.Empty(t, vc.req.Reason, "a denied permission places no call")

	offline := NewVoiceCallTools(func() (VoiceCalls, error) { return nil, voice.ErrNotRunning }, perms)
	resp, err := voiceTool(t, offline, CallOwnerToolName).Run(context.Background(), fantasy.ToolCall{ID: "t2", Input: `{"reason":"x"}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "phone serve")

	vc.err = errors.New("boom")
	perms.allow = true
	resp = run(`{"reason":"hata"}`)
	require.True(t, resp.IsError)
}

func TestReadOnlyVoiceTools(t *testing.T) {
	t.Parallel()
	vc := &fakeVoiceCalls{}
	ts := NewVoiceCallTools(func() (VoiceCalls, error) { return vc, nil }, &decidingPermissions{})
	resp, err := voiceTool(t, ts, CallTranscriptToolName).Run(context.Background(), fantasy.ToolCall{Input: `{"call_id":"c1"}`})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "storage is turned off")

	resp, err = voiceTool(t, ts, SiteMonitorToolName).Run(context.Background(), fantasy.ToolCall{Input: `{"action":"notify_on_recovery","site":"shop"}`})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "called when shop recovers: true")
	resp, err = voiceTool(t, ts, SiteMonitorToolName).Run(context.Background(), fantasy.ToolCall{Input: `{"action":"status"}`})
	require.NoError(t, err)
	require.Contains(t, resp.Content, `"health": "down"`)
	for _, name := range []string{CallStatusToolName, CallHistoryToolName, CallTranscriptToolName} {
		require.Equal(t, permission.CategoryReadOnly, permission.CategoryForTool(name))
	}
	require.Equal(t, permission.CategoryNetwork, permission.CategoryForTool(CallOwnerToolName))
}
