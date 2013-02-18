package main

import (
	"errors"

	"launchpad.net/gnuflag"
	"launchpad.net/juju-core/cmd"
	"launchpad.net/juju-core/juju"
)

// UnexposeCommand is responsible exposing services.
type UnexposeCommand struct {
	EnvName     string
	ServiceName string
}

func (c *UnexposeCommand) Info() *cmd.Info {
	return &cmd.Info{"unexpose", "", "unexpose a service", ""}
}

func (c *UnexposeCommand) SetFlags(f *gnuflag.FlagSet) {
	addEnvironFlags(&c.EnvName, f)
}

func (c *UnexposeCommand) Init(f *gnuflag.FlagSet, args []string) error {
	if err := f.Parse(true, args); err != nil {
		return err
	}
	args = f.Args()
	if len(args) == 0 {
		return errors.New("no service name specified")
	}
	c.ServiceName = args[0]
	return cmd.CheckEmpty(args[1:])
}

// Run changes the juju-managed firewall to hide any
// ports that were also explicitly marked by units as closed.
func (c *UnexposeCommand) Run(_ *cmd.Context) error {
	conn, err := juju.NewConnFromName(c.EnvName)
	if err != nil {
		return err
	}
	defer conn.Close()
	svc, err := conn.State.Service(c.ServiceName)
	if err != nil {
		return err
	}
	return svc.ClearExposed()
}
