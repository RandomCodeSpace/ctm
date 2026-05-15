package main

import (
	"github.com/RandomCodeSpace/ctm/cmd"

	// Side-effect import: codex.init() registers the codex Agent with the
	// internal/agent registry. Without this blank import the ctm binary
	// would link with no agents registered and any attach / yolo / check
	// would fail at agent.For lookup.
	_ "github.com/RandomCodeSpace/ctm/internal/agent/codex"
	_ "github.com/RandomCodeSpace/ctm/internal/agent/hermes"
)

func main() {
	cmd.Execute()
}
