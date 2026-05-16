// Command axon is the CLI entry point.
//
// All logic lives in github.com/atakang7/axon/agent; this file just
// wires the binary to agent.Main.
package main

import "github.com/atakang7/axon/agent"

func main() { agent.Main() }
