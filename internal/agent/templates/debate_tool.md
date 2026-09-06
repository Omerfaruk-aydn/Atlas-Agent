Put a question to several agents and let them argue it out over more than one round, then get back where they ended up and what they still disagree about.

This is the escalation from `orchestrate`. `orchestrate` asks the same question of several agents once, independently, and hands you the answers to compare: good when you want corroboration, and a majority answer means something precisely because none of them saw the others. `debate` gives them each other's answers and asks them to respond -- to defend, concede, or change position -- which is what you want when the first round disagreed and you need to know *why*, or when the question is one where an unexamined answer is not worth much.

Use it when:
- You are stuck, and the ways forward you can see all have something wrong with them.
- The first `orchestrate` came back split, and the split is the thing you need resolved.
- A decision is expensive to get wrong and cheap to think about longer: an architecture you will build on, a migration, a security call, a fix you cannot easily undo.
- Something looks right but you cannot say why, and want it attacked before you build on it.

Do not use it for work with a knowable answer. A question the codebase settles is settled by reading the codebase; three models speculating about it produce three confident guesses and no evidence. Nor for routine work: a debate costs `agent_names` × `rounds` model calls, and on a question with an obvious answer that buys agreement you already had.

<parameters>
- `question` — what is actually being decided, with the context needed to decide it. Each agent sees only this, never your conversation: state the constraints, what has been tried, and what "better" means here. A vague question produces a vague argument.
- `agent_names` — two to five configured subagents, from the list at the end of this description. Pick ones that will disagree: a security agent and a frontend agent bring different objections to the same design, where two general agents mostly agree with each other. Assigning them different models sharpens this further -- see the `atlas_config` tool.
- `rounds` — how many times they see each other and respond. Default 2, maximum 4. Two is usually right: one round to stake out positions, one to answer the others. Beyond that they mostly converge on wording.
- `judge_agent` — an agent, distinct from the others, that reads the whole exchange and writes the conclusion. Optional; without it you get the final positions and decide yourself.
</parameters>

<what_comes_back>
Every agent's final position, labelled, plus the judge's conclusion when you asked for one. Disagreement that survived the debate is reported as disagreement rather than averaged away -- two agents still holding opposite positions after arguing is a real finding about the question, and flattening it into a consensus that does not exist would be the one outcome worse than not asking.
</what_comes_back>

<tips>
- Say what the debate concluded and, when it stayed split, that it stayed split and on what. Do not present a contested answer as settled.
- The agents cannot see your files unless the question tells them what is in them. Paste the relevant code or contract into the question.
- One round with `orchestrate` first is often enough. Reach for `debate` when that round disagrees, not before.
</tips>
