func TestRemoteQualityRevisedCandidateDoesNotReuseOldPackage(t *testing.T) {
	dir := t.TempDir()
	s := mobileSystem()
	_, err := runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "set", System: &s}, time.Now)
	require.NoError(t, err)
	writePNG(t, filepath.Join(dir, "screen.png"), 393, 852)
	p := DesignSystemParams{Action: "package", Name: "draft", Screens: []string{"screen.png"}}
	_, err = runDesignSystem(t.Context(), dir, p, time.Now)
	require.NoError(t, err)
	s.Direction.Name = "Completely different visual direction"
	s.Colors["accent"] = "#FF0000"
	_, err = runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "set", System: &s}, time.Now)
	require.NoError(t, err)
	_, err = runDesignSystem(t.Context(), dir, p, time.Now)
	require.Error(t, err, "changed candidate tokens must not be reported as the old unchanged delivery")
}

func TestRemoteQualityChangedOpenQuestionsDoesNotReuseOldPackage(t *testing.T) {
	dir := t.TempDir()
	s := mobileSystem()
	_, err := runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "set", System: &s}, time.Now)
	require.NoError(t, err)
	writePNG(t, filepath.Join(dir, "screen.png"), 393, 852)
	p := DesignSystemParams{Action: "package", Name: "draft", Screens: []string{"screen.png"}}
	_, err = runDesignSystem(t.Context(), dir, p, time.Now)
	require.NoError(t, err)
	p.OpenQuestions = []string{"Contrast and overflow were not verified"}
	_, err = runDesignSystem(t.Context(), dir, p, time.Now)
	require.Error(t, err, "new limitations must not silently disappear in an unchanged package")
}

func TestRemoteQualityUnknownPowerEffectStaysUnknown(t *testing.T) {
	tool := &remoteSupportTool{}
	res := supportResult{OK: false, Device: supportDevice{RemoteDeviceID: "dev-review", SessionID: "ss-review"}, Effect: support.EffectUnknown}
	err := &support.Error{Code: support.CodeOutcomeUnknown, InputSent: true, Effect: support.EffectUnknown}
	got, _ := tool.powerOutcome(powerShutdown, res, err, "op-review")
	require.Equal(t, support.EffectUnknown, got.Effect, "a dropped socket is not evidence that the OS executed shutdown")
}

func TestRemoteQualityPowerDetectionIgnoresQuotedOutput(t *testing.T) {
	_, ok := classifyPowerCommand([]string{"powershell", "-Command", "Write-Output 'Stop-Computer'"})
	require.False(t, ok, "printing the name of a power command is not executing it")
}

func TestRemoteQualitySecondImageWrapperKeepsOriginalPreviewCoordinates(t *testing.T) {
	data := paddedPNG(t, 1920, 1080, MaxImageDeliveryBytes+1)
	d, err := PrepareImageForDelivery(data, "image/png")
	require.NoError(t, err)
	require.True(t, d.Preview)
	resp := fantasy.WithResponseMetadata(fantasy.NewImageResponse(d.Data, d.MediaType), map[string]any{"image_delivery": d})
	got := EnforceImageDelivery(resp)
	var meta struct {
		Image ImageDelivery `json:"image_delivery"`
	}
	require.NoError(t, json.Unmarshal([]byte(got.Metadata), &meta))
	require.Equal(t, d.CoordinateScale, meta.Image.CoordinateScale, "outer wrapper must not reset preview scaling")
	require.True(t, meta.Image.Preview)
	require.Equal(t, 1920, meta.Image.OriginalWidth)
}

func TestRemoteQualityDesignWritesParticipateInMutationGuard(t *testing.T) {
	for _, action := range []string{"set", "approve", "reset", "package"} {
		require.True(t, CallMutates(DesignSystemToolName, `{"action":"`+action+`"}`), action+" writes project state")
	}
}
