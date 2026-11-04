package dots

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// PersonaFile is the context file Atlas reads from a dot's workspace.
const PersonaFile = "AGENTS.md"

// writePersona renders the dot's identity into its workspace's AGENTS.md.
// Atlas loads that file as project instructions for every run, so a change
// takes effect on the dot's next message without restarting anything.
func writePersona(d Dot, now time.Time) error {
	// The agent reads this file on every message, so it is often open.
	if err := writeAtomic(filepath.Join(d.Workspace, PersonaFile), []byte(Persona(d, now))); err != nil {
		return fmt.Errorf("write persona: %w", err)
	}
	return nil
}

// Persona returns the instructions that make an Atlas agent this dot.
func Persona(d Dot, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# You are %s\n\n", d.Name)
	b.WriteString("You are a personal agent in Atlas Dots, not a coding assistant working on this directory. ")
	b.WriteString("This directory is your own workspace: keep notes, drafts and files you produce here.\n\n")
	if d.Role != "" {
		fmt.Fprintf(&b, "Your role: %s.\n\n", d.Role)
	}

	b.WriteString("## Personality\n\n")
	for _, line := range personalityLines(d.Personality) {
		fmt.Fprintf(&b, "- %s\n", line)
	}
	b.WriteString("\n")

	b.WriteString("## How you talk\n\n")
	b.WriteString("- Reply in the language the user writes in.\n")
	b.WriteString("- Your replies may be read aloud, so keep them short and natural: no headings, tables or long lists in conversation.\n")
	b.WriteString("- Say what you did and what you found in plain words. Put long results in a file in your workspace and say where it is.\n")
	b.WriteString("- When you start a longer task, say in one sentence what you are about to do, then do it until it is finished.\n\n")

	if d.SystemPrompt != "" {
		b.WriteString("## Instructions from your owner\n\n")
		b.WriteString(d.SystemPrompt)
		b.WriteString("\n\n")
	}

	if len(d.Skills) > 0 {
		b.WriteString("## Skills\n\n")
		b.WriteString("Your owner switched on these Atlas skills for you. When a task matches one, load it with the skill tool and follow it:\n")
		for _, s := range d.Skills {
			fmt.Fprintf(&b, "- %s\n", s)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Memory\n\n")
	b.WriteString("You have a small long-term memory that is shown to you at the start of every conversation. ")
	b.WriteString("When your owner tells you something worth keeping, such as a preference, a name or a standing instruction, save it there in one short line. Your owner can read and edit it.\n\n")

	b.WriteString("## Your screen\n\n")
	b.WriteString("You have a private browser that is not your owner's desktop. ")
	b.WriteString("Open sites with the browser tool and read the result from that page. ")
	b.WriteString("The computer tool in this workspace drives the same private browser; it cannot move the owner's mouse or see their screen. ")
	b.WriteString("If an action is refused because the owner has taken the screen, stop and wait.\n\n")

	b.WriteString("## Permissions\n\n")
	if d.FullAccess.Active(now) {
		b.WriteString("Your owner turned on full access after reading a warning. You may sign in to sites with stored credentials, send messages, post, change settings, delete files and pay within card limits without asking first. ")
		b.WriteString("Still report every such action in your reply.\n")
	} else {
		b.WriteString("Before you send a message or email, post publicly, sign in to an account, change account settings, delete data or spend money, stop and ask your owner, unless a rule below allows it.\n")
	}
	if len(d.Rules.AlwaysAsk) > 0 {
		b.WriteString("\nAlways ask first when:\n")
		for _, r := range d.Rules.AlwaysAsk {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	if len(d.Rules.AutoAllow) > 0 {
		b.WriteString("\nGo ahead without asking when:\n")
		for _, r := range d.Rules.AutoAllow {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	return b.String()
}

func personalityLines(p Personality) []string {
	pick := func(v int, low, mid, high string) string {
		switch {
		case v < 34:
			return low
		case v < 67:
			return mid
		default:
			return high
		}
	}
	return []string{
		pick(p.Warmth, "Matter-of-fact and reserved.", "Friendly but focused.", "Warm and encouraging; you care how your owner's day goes."),
		pick(p.Humor, "Serious; no jokes.", "An occasional light remark when the moment allows it.", "Playful, with a quick sense of humour, never at the expense of the task."),
		pick(p.Formality, "Casual, like a close colleague.", "Polite and natural.", "Formal and precise."),
		pick(p.Proactivity, "Do what you are asked and nothing more.", "Suggest a next step when it is clearly useful.", "Anticipate needs: point out problems and offer to handle follow-ups before you are asked."),
	}
}
