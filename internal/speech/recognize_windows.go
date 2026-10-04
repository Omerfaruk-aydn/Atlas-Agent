//go:build windows

package speech

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
	"unicode/utf16"
)

//go:embed recognize.ps1
var recognizerScript string

type nativeResult struct {
	Status
	Text      string `json:"text"`
	ErrorCode string `json:"error_code"`
}

type limitedOutput struct{ bytes.Buffer }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 256*1024 {
		return 0, errors.New("speech helper output exceeds limit")
	}
	return b.Buffer.Write(p)
}

func recognize(ctx context.Context, path string, o DictationOptions) (nativeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	data, err := runSpeechHelper(ctx, recognizerScript, map[string]string{"path": path, "language": o.Language})
	if err != nil {
		return nativeResult{}, err
	}
	return decodeResult(data)
}

func runSpeechHelper(ctx context.Context, script string, payload any) ([]byte, error) {
	units := utf16.Encode([]rune(script))
	encoded := make([]byte, len(units)*2)
	for i, v := range units {
		binary.LittleEndian.PutUint16(encoded[i*2:], v)
	}
	input, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded))
	hideWindow(cmd)
	cmd.Stdin = bytes.NewReader(input)
	var output limitedOutput
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("offline Windows speech helper failed")
	}
	return output.Bytes(), nil
}

func decodeResult(data []byte) (nativeResult, error) {
	var result nativeResult
	if err := json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &result); err != nil {
		return result, errors.New("invalid Windows speech helper response")
	}
	switch result.ErrorCode {
	case "":
	case "no_recognizer", "language_unavailable":
		return result, fmt.Errorf("%w; installed languages: %v", ErrNoRecognizer, result.Installed)
	case "recognition_timeout":
		return result, errors.New("offline speech recognition timed out")
	default:
		return result, errors.New("offline Windows speech recognition failed")
	}
	return result, nil
}

func check(ctx context.Context, o DictationOptions) (Status, error) {
	if o.Backend != "windows" {
		return checkVosk(ctx, o)
	}
	r, err := recognize(ctx, "", o)
	r.Backend = "windows"
	return r.Status, err
}

func transcribe(ctx context.Context, path string, o DictationOptions) (string, error) {
	if o.Backend != "windows" {
		return transcribeVosk(ctx, path, o)
	}
	r, err := recognize(ctx, path, o)
	return r.Text, err
}
