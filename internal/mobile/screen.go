			}
			if !write(p) {
				return
			}
		case <-tick.C:
			// Revocation, logout, expiry and turning viewing off all end
			// the stream within a second.
			dev, err := g.Auth.Authenticate(cookie.Value)
			if err != nil || dev.ID != d.ID || !dev.Previews {
				state := map[bool]string{true: "revoked", false: "previews_off"}[err != nil || dev.ID != d.ID]
				write(&screenPacket{kind: screenKindStatus, captured: time.Now(), payload: mustJSON(screenStats{State: state})})
				return
			}
		}
	}
}

func mustJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}
