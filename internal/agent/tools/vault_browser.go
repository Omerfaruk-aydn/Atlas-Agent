package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
)

func fillBrowserVault(ctx context.Context, sess browser.Session, v *vault.Store, params BrowserParams) (fantasy.ToolResponse, error) {
	current, err := sess.URL()
	if err != nil {
		return fantasy.NewTextErrorResponse("Cannot observe credential origin"), nil
	}
	u, err := url.Parse(current)
	if err != nil {
		return fantasy.NewTextErrorResponse("Invalid page URL"), nil
	}
	selector, err := resolveTargetSelector("type", params)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	err = v.Fill(ctx, params.CredentialID, u.Scheme+"://"+u.Host, GetSessionFromContext(ctx), func(origin, password string) error {
		o, _ := json.Marshal(origin)
		p, _ := json.Marshal(password)
		s, _ := json.Marshal(selector)
		script := `(()=>{if(location.origin!==` + string(o) + `){return "refused"};const els=document.querySelectorAll(` + string(s) + `);if(els.length!==1){return "refused"};const e=els[0];if(!(e instanceof HTMLInputElement)||e.type!=="password"||e.disabled||e.readOnly||!e.getClientRects().length||getComputedStyle(e).visibility!=="visible"||Number(getComputedStyle(e).opacity)===0){return "refused"};const set=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,"value").set;set.call(e,` + string(p) + `);e.dispatchEvent(new Event("input",{bubbles:true}));e.dispatchEvent(new Event("change",{bubbles:true}));return "filled"})()`
		result, err := sess.Eval(script)
		if err != nil || result != "filled" && result != `"filled"` {
			return errors.New("fill refused")
		}
		return nil
	})
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	return fantasy.NewTextResponse("Password filled into the matching HTTPS origin. No password was returned. Verify login separately."), nil
}
