package main

import (
	"github.com/alecthomas/kong"
	"github.com/block/checks4shell/cmd"
)

func main() {
	c := &cmd.Checks4shell{}
	ctx := kong.Parse(c, kong.UsageOnError())

	err := ctx.Run(c)
	ctx.FatalIfErrorf(err)
}
