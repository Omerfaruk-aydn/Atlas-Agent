			found = true
			require.Equal(t, "image/png", part.InlineData.MIMEType)
			require.Equal(t, raw, part.InlineData.Data)
		}
	}
	require.True(t, found)
}
