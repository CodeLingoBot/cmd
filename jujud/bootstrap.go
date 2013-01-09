package main

import (
	"encoding/base64"
	"fmt"
	"launchpad.net/gnuflag"
	"launchpad.net/goyaml"
	"launchpad.net/juju-core/cmd"
	"launchpad.net/juju-core/environs/config"
	"launchpad.net/juju-core/state"
)

type BootstrapCommand struct {
	Conf       AgentConf
	InstanceId string
	EnvConfig  map[string]interface{}
}

// Info returns a decription of the command.
func (c *BootstrapCommand) Info() *cmd.Info {
	return &cmd.Info{"bootstrap-state", "", "initialize juju state.", ""}
}

// Init initializes the command for running.
func (c *BootstrapCommand) Init(f *gnuflag.FlagSet, args []string) error {
	c.Conf.addFlags(f)
	f.StringVar(&c.InstanceId, "instance-id", "", "instance id of this machine")
	yamlBase64Var(f, &c.EnvConfig, "env-config", "", "initial environment configuration (yaml, base64 encoded)")
	if err := f.Parse(true, args); err != nil {
		return err
	}
	if c.InstanceId == "" {
		return requiredError("instance-id")
	}
	if len(c.EnvConfig) == 0 {
		return requiredError("env-config")
	}
	return c.Conf.checkArgs(f.Args())
}

// Run initializes state for an environment.
func (c *BootstrapCommand) Run(_ *cmd.Context) error {
	if err := c.Conf.read("bootstrap"); err != nil {
		return err
	}
	cfg, err := config.New(c.EnvConfig)
	if err != nil {
		return err
	}
	st, err := state.Initialize(&c.Conf.StateInfo, cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	m, err := st.InjectMachine(state.InstanceId(c.InstanceId), state.JobManageEnviron)
	if err != nil {
		return err
	}
	if c.Conf.OldPassword != "" {
		if err := m.SetPassword(c.Conf.OldPassword); err != nil {
			return err
		}
		if err := st.SetAdminPassword(c.Conf.OldPassword); err != nil {
			return err
		}
	}
	return nil
}

// yamlBase64Value implements gnuflag.Value on a map[string]interface{}.
type yamlBase64Value map[string]interface{}

// Set decodes the base64 value into yaml then expands that into a map.
func (v *yamlBase64Value) Set(value string) error {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	return goyaml.Unmarshal(decoded, v)
}

func (v *yamlBase64Value) String() string {
	return fmt.Sprintf("%v", *v)
}

// yamlBase64Var sets up a gnuflag flag analogous to the FlagSet.*Var methods.
func yamlBase64Var(fs *gnuflag.FlagSet, target *map[string]interface{}, name string, value string, usage string) {
	fs.Var((*yamlBase64Value)(target), name, usage)
}
