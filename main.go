package main

import (
	"github.com/chupakbra/proxmox-cli/cli"
)

var version = "dev"

func main() {
	cli.Execute(version)
}
