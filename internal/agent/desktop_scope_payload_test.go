package agent

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestGUIOnlyTaskDoesNotAdvertiseForbiddenToolsAcrossProviders(t *testing.T) {
	for _, format := range []wireFormat{wireOpenAI, wireAnthropic, wireGemini} {
		for _, options := range []string{"", `, "defer_tool_schemas": false`} {
			t.Run(string(format)+"/"+options, func(t *testing.T) {
				h := newPayloadCoordinator(t, format, options, automationTools,
					textReply(format, "Ready"), textReply(format, "Code ready"))
				session := newSession(t, h)
				_, err := h.coord.Run(t.Context(), session, "Computer use ile Not Defteri uygulamasını aç ve sonucu kaydet.")
				require.NoError(t, err)
				names := stepTools(t, h)[0]
				for _, name := range []string{"bash", "edit", "write", "multiedit", "execution", "apply_patch"} {
					require.NotContains(t, names, name, "do not offer a tool the runtime will refuse in this task")
				}
				require.Contains(t, names, "computer")
				require.Contains(t, names, "tool_pipeline")
				require.Contains(t, stepBody(t, h, 0), "Desktop execution priorities")
				require.Contains(t, stepBody(t, h, 0), `\"result\":\"checkpoints\"`)
				_, err = h.coord.Run(t.Context(), session, "Now fix main.go and run go test.")
				require.NoError(t, err)
				for _, name := range []string{"bash", "edit", "write"} {
					require.Contains(t, stepTools(t, h)[1], name, "new coding tasks regain their normal tools")
				}
			})
		}
	}
}

// Exercise the example actually delivered to every provider, rather than
// asserting that some independently maintained sample has valid syntax.
func TestDesktopProtocolCalculationExampleRunsThroughPipeline(t *testing.T) {
	_, code, found := strings.Cut(desktopProtocol, "```json\n")
	require.True(t, found)
	code, _, found = strings.Cut(code, "\n```")
	require.True(t, found)
	for _, rejectSecond := range []bool{false, true} {
		t.Run(strconv.FormatBool(rejectSecond), func(t *testing.T) {
			value, inputs, checks, windows := 0, 0, 0, 0
			pipeline := tools.NewToolPipeline(func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				require.Equal(t, tools.ComputerToolName, call.Name)
				var input tools.ComputerParams
				require.NoError(t, json.Unmarshal([]byte(call.Input), &input))
				switch input.Action {
				case "type":
					inputs++
					text := strings.TrimSuffix(input.Text, "=")
					if lhs, rhs, ok := strings.Cut(text, "*"); ok {
						a, err := strconv.Atoi(lhs)
						require.NoError(t, err)
						b, err := strconv.Atoi(rhs)
						require.NoError(t, err)
						value = a * b
					} else {
						operand, err := strconv.Atoi(text[1:])
						require.NoError(t, err)
						switch text[0] {
						case '/':
							value /= operand
						case '+':
							value += operand
						default:
							t.Fatalf("Unsupported example input: %q", text)
						}
					}
					if rejectSecond && inputs == 2 {
						value++
					}
					return fantasy.NewTextResponse("Typed"), nil
				case "assert":
					checks++
					require.Equal(t, "12345", input.Automation.WindowID)
					require.Equal(t, "42:12345:4:10", input.Automation.ElementID)
					digits := strconv.Itoa(value)
					for i := len(digits) - 3; i > 0; i -= 3 {
						digits = digits[:i] + "." + digits[i:]
					}
					actual := "Ekran değeri " + digits
					passed := actual == input.Automation.Expected
					data, err := json.Marshal(map[string]any{"passed": passed, "actual": actual, "window_id": "12345"})
					require.NoError(t, err)
					if !passed {
						return fantasy.NewTextErrorResponse(string(data)), nil
					}
					return fantasy.NewTextResponse(string(data)), nil
				case "windows":
					windows++
					return fantasy.NewTextResponse(`{"result":[{"window_id":"12345","foreground":true}]}`), nil
				default:
					t.Fatalf("Example added an unnecessary image/tree call: %s", input.Action)
					return fantasy.ToolResponse{}, nil
				}
			})
			result, err := pipeline.Run(t.Context(), fantasy.ToolCall{Name: "tool_pipeline", ID: "example", Input: code})
			require.NoError(t, err)
			if rejectSecond {
				require.True(t, result.IsError, result.Content)
				require.Equal(t, 2, inputs, "failed intermediate verification stops the last calculation")
				require.Equal(t, 2, checks)
				require.Zero(t, windows)
			} else {
				require.False(t, result.IsError, result.Content)
				require.Equal(t, 3, inputs)
				require.Equal(t, 3, checks)
				require.Equal(t, 1, windows)
				require.Contains(t, result.Content, `"actual":"Ekran değeri 9.000"`)
			}
		})
	}
}
