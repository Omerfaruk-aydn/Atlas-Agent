package design

import (
	"fmt"
	"strings"
	"time"
)

// Reply is one message under a comment.
type Reply struct {
	Author  string `json:"author"`
	Text    string `json:"text"`
	Created string `json:"created"`
}

// Comment is a note pinned to a place on a page, with its replies. The
// person leaves them in the app and the agent reads and answers them.
type Comment struct {
	ID string `json:"id"`
	// Page is the id of the page the pin is on; X and Y are page coordinates.
	Page string  `json:"page"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	// Node is the layer the comment is about, when it has one.
	Node     string  `json:"node,omitempty"`
	Author   string  `json:"author"`
	Text     string  `json:"text"`
	Created  string  `json:"created"`
	Resolved bool    `json:"resolved,omitempty"`
	Replies  []Reply `json:"replies,omitempty"`
}

const maxComments = 500

func sanitizeComments(d *Doc) {
	pages := map[string]bool{}
	for _, p := range d.Pages {
		pages[p.ID] = true
	}
	seen := map[string]bool{}
	var out []Comment
	for _, c := range d.Comments {
		if !validRef(c.ID) || seen[c.ID] || len(out) == maxComments {
			continue
		}
		if !pages[c.Page] {
			continue
		}
		seen[c.ID] = true
		c.X = clamp(c.X, -limit, limit)
		c.Y = clamp(c.Y, -limit, limit)
		if !validRef(c.Node) {
			c.Node = ""
		}
		c.Author = cut(strings.TrimSpace(c.Author), 80)
		if c.Author == "" {
			c.Author = "person"
		}
		c.Text = cut(c.Text, 4000)
		var replies []Reply
		for _, r := range c.Replies {
			if len(replies) == 200 {
				break
			}
			r.Author = cut(strings.TrimSpace(r.Author), 80)
			if r.Author == "" {
				r.Author = "person"
			}
			if r.Text = cut(r.Text, 4000); r.Text == "" {
				continue
			}
			replies = append(replies, r)
		}
		c.Replies = replies
		out = append(out, c)
	}
	d.Comments = out
}

func (d *Doc) comment(id string) *Comment {
	for i := range d.Comments {
		if d.Comments[i].ID == id {
			return &d.Comments[i]
		}
	}
	return nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// commentOp runs the ops that edit comments. It reports false for any
// other op.
func (d *Doc) commentOp(i int, op Op, seen map[string]bool, res *Result, defaultPage string) (bool, error) {
	switch op.Op {
	case "add_comment":
		if strings.TrimSpace(op.Text) == "" {
			return true, fmt.Errorf("a comment needs text")
		}
		page, err := d.pageFor(op, defaultPage)
		if err != nil {
			return true, err
		}
		if len(d.Comments) >= maxComments {
			return true, fmt.Errorf("at most %d comments", maxComments)
		}
		c := Comment{ID: d.freshID(seen), Page: page.ID, Author: op.Author, Text: op.Text, Created: now(), Node: op.NodeID}
		if c.Author == "" {
			c.Author = "agent"
		}
		if op.NodeID != "" {
			at := d.Find(op.NodeID)
			if at == nil {
				return true, fmt.Errorf("no node %q to comment on", op.NodeID)
			}
			bx, by, _, _ := Bounds(World(at.Path), at.Node)
			c.Page, c.X, c.Y = at.Page.ID, bx, by
		}
		if op.X != nil {
			c.X = *op.X
		}
		if op.Y != nil {
			c.Y = *op.Y
		}
		d.Comments = append(d.Comments, c)
		res.Created[i] = c.ID
		return true, nil
	case "reply_comment":
		c := d.comment(op.ID)
		if c == nil {
			return true, fmt.Errorf("no comment %q", op.ID)
		}
		if strings.TrimSpace(op.Text) == "" {
			return true, fmt.Errorf("a reply needs text")
		}
		author := op.Author
		if author == "" {
			author = "agent"
		}
		c.Replies = append(c.Replies, Reply{Author: author, Text: op.Text, Created: now()})
		return true, nil
	case "resolve_comment":
		c := d.comment(op.ID)
		if c == nil {
			return true, fmt.Errorf("no comment %q", op.ID)
		}
		c.Resolved = op.Resolved == nil || *op.Resolved
		return true, nil
	case "delete_comment":
		for k := range d.Comments {
			if d.Comments[k].ID == op.ID {
				d.Comments = append(d.Comments[:k], d.Comments[k+1:]...)
				return true, nil
			}
		}
		return true, fmt.Errorf("no comment %q", op.ID)
	}
	return false, nil
}

// OpenComments describes the unresolved comments, for the agent to read.
func OpenComments(d *Doc) string {
	var b strings.Builder
	for _, c := range d.Comments {
		if c.Resolved {
			continue
		}
		where := fmt.Sprintf("page %s at %s,%s", c.Page, num(c.X), num(c.Y))
		if c.Node != "" {
			where += " on node " + c.Node
		}
		fmt.Fprintf(&b, "- comment %s by %s (%s): %q\n", c.ID, c.Author, where, c.Text)
		for _, r := range c.Replies {
			fmt.Fprintf(&b, "    %s replied: %q\n", r.Author, r.Text)
		}
	}
	return b.String()
}

// Comment is the comment with this id, or nil.
func (d *Doc) Comment(id string) *Comment { return d.comment(id) }
