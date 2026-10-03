package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/experiments"
)

//go:embed ui_audit.md
var uiAuditDescription string

const basicAccessibilityScript = `(async()=>{
if(window.axe&&typeof window.axe.run==='function'){
 const result=await window.axe.run(document,{resultTypes:['violations','incomplete']});
 return {engine:'axe-core',version:window.axe.version,violations:result.violations.slice(0,100).map(v=>({id:v.id,impact:v.impact,description:v.description,nodes:v.nodes.slice(0,20).map(n=>({target:n.target,summary:n.failureSummary}))})),incomplete:result.incomplete.length,truncated:result.violations.length>100};
}
const nodes=Array.from(document.querySelectorAll('img,input,select,textarea,button,a[href]'));
const visible=e=>e.getClientRects().length>0;
const name=e=>e.getAttribute('aria-label')||((e.getAttribute('aria-labelledby')||'').split(/\s+/).map(id=>document.getElementById(id)?.textContent||'').join(' ').trim())||(e.labels&&Array.from(e.labels).map(l=>l.textContent).join(' ').trim())||e.textContent.trim()||e.getAttribute('title')||'';
const violations=[];
for(const e of nodes.slice(0,2000)){
 if(!visible(e)||e.getAttribute('aria-hidden')==='true')continue;
 if(e.tagName==='IMG'&&!e.hasAttribute('alt'))violations.push({rule:'image-alt',tag:e.tagName,id:e.id});
 if(['INPUT','SELECT','TEXTAREA'].includes(e.tagName)&&!['hidden','submit','button','image'].includes(e.type)&&!name(e))violations.push({rule:'input-label',tag:e.tagName,id:e.id});
 if(['BUTTON','A'].includes(e.tagName)&&!name(e))violations.push({rule:'control-name',tag:e.tagName,id:e.id});
}
if(!document.documentElement.lang)violations.push({rule:'page-language'});
return {engine:'bounded-dom-checks',partial:true,limitations:['contrast and full accessible-name computation require axe-core','manual accessibility checks remain necessary'],truncated:nodes.length>2000,violations:violations.slice(0,100)};
})()`

const interactionAuditScript = `(()=>{const nodes=Array.from(document.querySelectorAll('button,a[href],input,select,textarea,[tabindex]')).filter(e=>e.getClientRects().length>0);return {url:location.href,width:innerWidth,height:innerHeight,horizontal_overflow:document.documentElement.scrollWidth>innerWidth,active_element:{tag:document.activeElement?.tagName,id:document.activeElement?.id},controls:nodes.slice(0,200).map(e=>({tag:e.tagName,id:e.id,tabindex:e.tabIndex,disabled:!!e.disabled})),truncated:nodes.length>200,partial:true,limitation:'observed DOM state; use ui_verify to exercise actual interactions'};})()`

type VisualDiffParams struct {
	BeforeID        string  `json:"before_id"`
	AfterID         string  `json:"after_id"`
	Tolerance       int     `json:"tolerance,omitempty" description:"Per-channel tolerance 0-255."`
	MaxChangedRatio float64 `json:"max_changed_ratio,omitempty" description:"Maximum accepted changed pixel ratio, 0-1."`
}

func NewUIAuditTools(store *engineering.Store, invoke ToolInvoker) []fantasy.AgentTool {
	var tools []fantasy.AgentTool
	for _, name := range []string{"a11y_audit", "interaction_audit"} {
		tools = append(tools, fantasy.NewAgentTool(name, uiAuditDescription, func(ctx context.Context, _ struct{}, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			script := basicAccessibilityScript
			if name == "interaction_audit" {
				script = interactionAuditScript
			}
			input, err := json.Marshal(BrowserParams{Action: "eval", Script: script})
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			return invoke(ctx, fantasy.ToolCall{ID: call.ID + "-audit", Name: BrowserToolName, Input: string(input)})
		}))
	}
	return append(tools, fantasy.NewAgentTool("visual_diff", uiAuditDescription, func(ctx context.Context, p VisualDiffParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if p.BeforeID == "" || p.AfterID == "" || p.Tolerance < 0 || p.Tolerance > 255 || math.IsNaN(p.MaxChangedRatio) || p.MaxChangedRatio < 0 || p.MaxChangedRatio > 1 {
			return fantasy.NewTextErrorResponse("invalid capture identity, tolerance or changed ratio"), nil
		}
		state, err := store.Read(ctx, GetSessionFromContext(ctx))
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		capture := func(id string) ([]byte, error) {
			for _, evidence := range state.UIEvidence {
				if evidence.ID != id {
					continue
				}
				if evidence.Target != "web" {
					return nil, fmt.Errorf("capture is not a web image")
				}
				if err := engineering.ValidateUIArtifact(ctx, evidence); err != nil {
					return nil, err
				}
				file, err := os.Open(evidence.Path)
				if err != nil {
					return nil, err
				}
				defer file.Close()
				data, err := io.ReadAll(io.LimitReader(file, 16*1024*1024+1))
				if err != nil {
					return nil, err
				}
				if len(data) > 16*1024*1024 || engineering.Hash(string(data)) != evidence.Hash {
					return nil, fmt.Errorf("capture changed during comparison")
				}
				return data, nil
			}
			return nil, fmt.Errorf("capture not found in current session")
		}
		before, err := capture(p.BeforeID)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		after, err := capture(p.AfterID)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		difference, err := experiments.CompareImages(ctx, before, after, uint32(p.Tolerance)*257)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		passed := difference.ChangedRatio <= p.MaxChangedRatio
		data, err := json.Marshal(map[string]any{"passed": passed, "difference": difference, "max_changed_ratio": p.MaxChangedRatio, "visual_inspection_required": true})
		result := fantasy.NewTextResponse(string(data))
		result.IsError = !passed
		return fantasy.WithResponseMetadata(result, MeasuredCheckMetadata{Observed: true, Passed: passed}), err
	}))
}
