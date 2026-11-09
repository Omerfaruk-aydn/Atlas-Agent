package design

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCommentsRoundTripThroughOps(t *testing.T) {
	t.Parallel()
	d := docFrom(t, `[{"id":"btn","type":"rect","x":40,"y":60,"width":100,"height":40}]`)
	out, res, err := Apply(d, "", mustOps(t, `[
	 {"op":"add_comment","text":"Make this bigger","author":"person","x":10,"y":20},
	 {"op":"add_comment","text":"Contrast?","node_id":"btn"}]`))
	require.NoError(t, err)
	require.Len(t, out.Comments, 2)
	require.Equal(t, "person", out.Comments[0].Author)
	require.Equal(t, "agent", out.Comments[1].Author)
	require.InDelta(t, 40, out.Comments[1].X, 1e-9)
	require.Equal(t, out.Pages[0].ID, out.Comments[1].Page)

	first := res.Created[0]
	out, _, err = Apply(out, "", mustOps(t, `[
	 {"op":"reply_comment","id":"`+first+`","text":"Done, 140px wide"},
	 {"op":"resolve_comment","id":"`+res.Created[1]+`"}]`))
	require.NoError(t, err)
	require.Len(t, out.Comment(first).Replies, 1)
	require.Contains(t, Outline(out, 50), "Make this bigger")
	require.NotContains(t, Outline(out, 50), "Contrast?", "resolved comments are not listed")

	_, _, err = Apply(out, "", mustOps(t, `[{"op":"reply_comment","id":"nope","text":"x"}]`))
	require.Error(t, err)
	_, _, err = Apply(out, "", mustOps(t, `[{"op":"add_comment","text":"  "}]`))
	require.Error(t, err)
	out, _, err = Apply(out, "", mustOps(t, `[{"op":"delete_comment","id":"`+first+`"}]`))
	require.NoError(t, err)
	require.Len(t, out.Comments, 1)
}

func TestCommentsOnMissingPagesAreDropped(t *testing.T) {
	t.Parallel()
	d, err := Parse([]byte(`{"pages":[{"id":"p","children":[]}],"comments":[{"id":"a","page":"p","text":"ok"},{"id":"b","page":"ghost","text":"lost"},{"id":"a","page":"p","text":"dup"}]}`))
	require.NoError(t, err)
	require.Len(t, d.Comments, 1)
	require.Equal(t, "person", d.Comments[0].Author)
}

func TestHistoryKeepsAgentEditsAndNamedVersions(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())
	doc := New("T")
	rev, err := store.Save("ui", doc, "", true)
	require.NoError(t, err)

	// A person's saves keep one version now and then, not one per save.
	for i := range 3 {
		doc.Name = "T" + string(rune('a'+i))
		rev, err = store.Save("ui", doc, rev, false)
		require.NoError(t, err)
	}
	versions, err := store.History("ui")
	require.NoError(t, err)
	require.Len(t, versions, 1)
	require.Equal(t, "auto", versions[0].Kind)

	// An agent edit keeps the state before it, every time.
	doc.Name = "after agent"
	_, err = store.SaveWith("ui", doc, rev, false, SaveOptions{Agent: true, Label: "agent 2 edits"})
	require.NoError(t, err)
	versions, _ = store.History("ui")
	require.Len(t, versions, 2)
	require.Equal(t, "agent", versions[0].Kind)
	require.Equal(t, "agent-2-edits", versions[0].Label)
	before, err := store.Version("ui", versions[0].ID)
	require.NoError(t, err)
	require.Equal(t, "Tc", before.Name)

	// A name keeps the current state, and renames a duplicate instead of copying it.
	require.NoError(t, store.SaveVersion("ui", "Launch review"))
	versions, _ = store.History("ui")
	require.Equal(t, "named", versions[0].Kind)
	require.Equal(t, "launch-review", versions[0].Label)
	require.Error(t, store.SaveVersion("ui", "  "))
	require.NoError(t, store.DeleteVersion("ui", versions[0].ID))
	_, err = store.Version("ui", "../../etc/passwd")
	require.Error(t, err)
	require.WithinDuration(t, time.Now(), versions[0].Time, time.Minute)
}

func TestGenerateCodeUsesFlexboxForAutoLayout(t *testing.T) {
	t.Parallel()
	d := docFrom(t, `[{"id":"card","type":"frame","name":"Pricing Card","width":300,"height":200,"radius":12,"fills":[{"type":"solid","color":"#FFFFFF"}],
	 "layout":{"dir":"column","gap":8,"pad":16},"children":[
	 {"id":"t","type":"text","text":"Pro \"plan\"","font":{"family":"Inter","size":20,"weight":700},"fills":[{"type":"solid","color":"#111111"}]},
	 {"id":"b","type":"rect","width":100,"height":40,"sizing_x":"fill","fills":[{"type":"solid","color":"#0055FF"}]},
	 {"id":"pin","type":"ellipse","absolute":true,"x":5,"y":6,"width":10,"height":10}]}]`)
	out, err := GenerateCode(d, "card", false)
	require.NoError(t, err)
	require.Contains(t, out.CSS, "display: flex;")
	require.Contains(t, out.CSS, "gap: 8px;")
	require.Contains(t, out.CSS, "padding: 16px 16px 16px 16px;")
	require.Contains(t, out.CSS, "width: 100%;")
	require.Contains(t, out.CSS, "position: absolute;")
	require.Contains(t, out.CSS, "border-radius: 50%;")
	require.Contains(t, out.Markup, "Pro &#34;plan&#34;")
	react, err := GenerateCode(d, "card", true)
	require.NoError(t, err)
	require.Contains(t, react.Markup, "export default function PricingCard()")
	require.Contains(t, react.Markup, "className=")
	_, err = GenerateCode(d, "missing", false)
	require.Error(t, err)
}
