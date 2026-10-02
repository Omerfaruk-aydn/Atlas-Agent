package shell

import (
	"fmt"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// Check isolated scripts before the host interpreter can execute a builtin.
func CheckIsolatedScript(command string, blocks []BlockFunc) error {
	return checkIsolatedScript(command, blocks)
}

func checkIsolatedScript(command string, blocks []BlockFunc) error {
	if len(blocks) == 0 {
		return nil
	}
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return err
	}
	syntax.Walk(file, func(node syntax.Node) bool {
		if err != nil {
			return false
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}
		for _, word := range call.Args {
			syntax.Walk(word, func(part syntax.Node) bool {
				switch part.(type) {
				case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp, *syntax.ProcSubst:
					err = fmt.Errorf("isolated command policy cannot validate dynamic command arguments")
				}
				return err == nil
			})
		}
		if err != nil {
			return false
		}
		var argv []string
		argv, err = expand.Fields(nil, call.Args...)
		if err != nil {
			return false
		}
		if len(argv) > 0 {
			switch argv[0] {
			case "eval", "source", ".":
				err = fmt.Errorf("isolated command policy cannot validate indirect shell source")
				return false
			}
		}
		for _, block := range blocks {
			if block(argv) {
				err = fmt.Errorf("command blocked by configured policy")
				return false
			}
		}
		return true
	})
	return err
}
