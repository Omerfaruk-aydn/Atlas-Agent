package google

import (
	"encoding/base64"
	"strings"
	"unicode/utf8"
)

// signaturePrefix marks a thought signature kept as base64 text.
const signaturePrefix = "b64:"

// skipSignature is the value Gemini accepts in place of a thought signature
// it cannot check, for a function call whose own signature was lost.
const skipSignature = "skip_thought_signature_validator"

// EncodeSignature turns a thought signature, opaque bytes, into the text
// kept in ReasoningMetadata. The bytes are not valid UTF-8, so kept as they
// are they would be replaced with U+FFFD when the message is saved as JSON,
// and Gemini would reject the signature as corrupted on the next turn.
func EncodeSignature(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	return signaturePrefix + base64.StdEncoding.EncodeToString(raw)
}

// DecodeSignature returns the bytes of a signature made by EncodeSignature.
// ok is false for one saved before that encoding whose bytes were lost.
func DecodeSignature(signature string) (raw []byte, ok bool) {
	if signature == "" {
		return nil, true
	}
	if encoded, found := strings.CutPrefix(signature, signaturePrefix); found {
		raw, err := base64.StdEncoding.DecodeString(encoded)
		return raw, err == nil
	}
	if !utf8.ValidString(signature) || strings.ContainsRune(signature, utf8.RuneError) {
		return nil, false
	}
	return []byte(signature), true
}

// FunctionCallSignature returns the signature to send with a function
// call: its own when it survived, the skip value when it was lost, and
// none when there was none.
func FunctionCallSignature(signature string) []byte {
	raw, ok := DecodeSignature(signature)
	if ok {
		return raw
	}
	// Gemini reads the field as base64; the skip value is written in the
	// URL-safe alphabet.
	skip, _ := base64.URLEncoding.DecodeString(skipSignature)
	return skip
}

// PartSignature returns the signature to send with a text or thought part,
// leaving out one whose bytes were lost.
func PartSignature(signature string) []byte {
	raw, _ := DecodeSignature(signature)
	return raw
}
