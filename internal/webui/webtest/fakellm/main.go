// Command fakellm serves the scripted model of package webtest and writes
// an isolated Atlas configuration that uses it, for browser end-to-end
// tests that run the real atlas-agent binary. It prints one JSON line with
// the model URL and the environment to start Atlas with, then serves until
// it is killed.
//
//	go run ./internal/webui/webtest/fakellm <root-dir>
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/webui/webtest"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: fakellm <root-dir>")
		os.Exit(2)
	}
	llm := webtest.NewFakeLLM()
	defer llm.Close()
	env, err := webtest.NewEnvironment(os.Args[1], llm.URL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, _ := json.Marshal(map[string]any{"url": llm.URL, "env": env.Env})
	fmt.Println(string(out))
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
}
