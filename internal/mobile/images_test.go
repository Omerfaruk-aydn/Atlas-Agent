	}}
	g.opts.Backend = &imageBackend{fakeBackend: backend, media: m}
	phone := newPhone(t, g)
	resp, _ := phone.do("GET", "/api/sessions/s1/messages/picture/images/0", nil, nil)
	require.Equal(t, 401, resp.StatusCode, "stored images require a paired phone")
	view := messageView(m)
	require.Empty(t, view["text"])
	require.Len(t, view["images"], 1)
	r := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
	r.SetPathValue("id", "s1")
	r.SetPathValue("message", "picture")
	r.SetPathValue("index", "0")
	w := httptest.NewRecorder()
	g.handleMessageImage(w, r)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, pngData.Bytes(), w.Body.Bytes())
	r.SetPathValue("id", "child")
	w = httptest.NewRecorder()
	g.handleMessageImage(w, r)
	require.Equal(t, 404, w.Code)
	for _, p := range []message.ContentPart{
		message.ImageURLContent{URL: "https://example.org/private.png"},
		message.BinaryContent{Path: "C:/private.png", MIMEType: "image/png"},
		message.BinaryContent{Data: pngData.Bytes(), MIMEType: "image/jpeg"},
		message.ToolResult{Data: "not base64", MIMEType: "image/png"},
		message.BinaryContent{Data: make([]byte, maxChatImageBytes+1), MIMEType: "image/png"},
	} {
		data, _ := chatImage(p)
		require.Nil(t, data)
	}
}
