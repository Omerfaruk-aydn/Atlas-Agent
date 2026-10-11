package support

import (
	"encoding/json"
	"net/http"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
)

// handleTool keeps built-in execution on the host and journals mutations.
func (h *Host) handleTool(w http.ResponseWriter, r *http.Request) {
	g, denied, status := h.Authorize(r, nodes.NeedInfo)
	if denied != nil {
		writeSupportJSON(w, status, denied)
		return
	}
	if h.opts.Executor == nil {
		writeSupportJSON(w, http.StatusConflict, apiErr2(CodeUnsupported, "remote built-in tools are unavailable on this host"))
		return
	}
	var req nodes.RemoteToolRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeSupportJSON(w, http.StatusBadRequest, apiErr2(CodeProtocol, "invalid remote tool request"))
		return
	}
	mutation, err := h.opts.Executor.Validate(req)
	if err != nil {
		writeSupportJSON(w, http.StatusBadRequest, apiErr2(CodeProtocol, err.Error()))
		return
	}
	var op *nodes.Operation
	if mutation {
		var existing bool
		op, existing, err = h.node.Journal().Begin(nodes.Operation{ID: req.OperationID, DeviceID: g.Device.ID, SessionID: g.SessionID, Generation: g.Generation, Kind: "tool_" + req.Tool, RequestHash: nodes.RequestHash(req)})
		if err != nil {
			writeSupportJSON(w, http.StatusConflict, apiErr2(CodeProtocol, err.Error()))
			return
		}
		if existing {
			var response fantasy.ToolResponse
			if op.State.Terminal() && json.Unmarshal([]byte(op.Stdout), &response) == nil {
				writeSupportJSON(w, http.StatusOK, nodes.RemoteToolReply{Operation: op, Response: response})
				return
			}
			writeSupportJSON(w, http.StatusConflict, &nodes.APIError{Code: string(CodeOutcomeUnknown), Message: "the remote workflow already started; inspect its journal or resume using its durable run_id", InputSent: true, Effect: "unknown"})
			return
		}
		h.OperationStarted(g, *op)
	}
	check := func(need nodes.Need) error {
		current, e, _ := h.Authorize(r, need)
		if e != nil {
			return FromAPI(e, h.id.NodeID)
		}
		if current.SessionID != g.SessionID || current.Generation != g.Generation {
			return Errorf(CodeStaleConnection, "the execution connection changed")
		}
		return nil
	}
	response, runErr := h.opts.Executor.Run(r.Context(), g, req, check)
	if runErr != nil {
		response = fantasy.NewTextErrorResponse(runErr.Error())
	}
	if op != nil {
		// Preserve the native stop/error contract for a replay, but never
		// persist screen pixels in the journal.
		saved := response
		saved.Data = nil
		if saved.Type == "image" {
			saved.Type, saved.MediaType = "text", ""
		}
		data, _ := json.Marshal(saved)
		final, finishErr := h.node.Journal().Finish(op.ID, func(o *nodes.Operation) {
			o.Stdout = string(data)
			o.State, o.Effect = taskstate.Completed, "applied"
			var metadata struct {
				Outcome struct {
					Dispatch string `json:"dispatch"`
				} `json:"atlas_outcome"`
			}
			_ = json.Unmarshal([]byte(response.Metadata), &metadata)
			if metadata.Outcome.Dispatch == "none" {
				o.Effect = "none"
			}
			if metadata.Outcome.Dispatch == "unknown" {
				o.State, o.Effect, o.Error = taskstate.Failed, "unknown", "the native tool could not determine the operation effect"
			}
			if response.IsError || response.StopTurn || runErr != nil {
				o.State, o.Error = taskstate.Failed, response.Content
				if metadata.Outcome.Dispatch != "none" {
					o.Effect = "unknown"
				}
			}
		})
		if finishErr != nil {
			writeSupportJSON(w, http.StatusInternalServerError, &nodes.APIError{Code: string(CodeOutcomeUnknown), Message: finishErr.Error(), InputSent: true, Effect: "unknown"})
			return
		}
		op = &final
		h.OperationFinished(g, final)
	}
	writeSupportJSON(w, http.StatusOK, nodes.RemoteToolReply{Operation: op, Response: response})
}
