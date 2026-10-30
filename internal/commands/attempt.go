package commands

// attemptCommands expose parallel attempts with a judge in the slash
// palette. The task stays literal data for the attempt tool, which asks for
// approval before running and again before applying a winner.
func attemptCommands() []CustomCommand {
	return []CustomCommand{
		{
			ID:   "attempt",
			Name: "attempt",
			Content: "Call the attempt tool with action run and exactly this task, using n 3 unless I asked otherwise: $TASK\n" +
				"Use cheap model roles for the attempts and a different, stronger judge if the configuration offers them. " +
				"When it returns, show me the ranked results, the evidence and the confidence with its reasons. " +
				"Do not apply anything until I agree; then call attempt with action apply for the run.",
			Arguments: []Argument{{ID: "TASK", Title: "Task", Description: "What every parallel attempt should accomplish", Required: true}},
		},
	}
}
