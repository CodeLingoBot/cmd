package main

import (
	"fmt"
	"launchpad.net/gnuflag"
	"launchpad.net/juju-core/charm"
	"launchpad.net/juju-core/cmd"
	"launchpad.net/juju-core/constraints"
	"launchpad.net/juju-core/environs"
	"launchpad.net/juju-core/environs/config"
	"launchpad.net/juju-core/version"
	"os"
	"strings"
)

// BootstrapCommand is responsible for launching the first machine in a juju
// environment, and setting up everything necessary to continue working.
type BootstrapCommand struct {
	EnvCommandBase
	Constraints constraints.Value
	UploadTools bool
	Series      []string
}

func (c *BootstrapCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name:    "bootstrap",
		Purpose: "start up an environment from scratch",
	}
}

func (c *BootstrapCommand) SetFlags(f *gnuflag.FlagSet) {
	c.EnvCommandBase.SetFlags(f)
	f.Var(constraints.ConstraintsValue{&c.Constraints}, "constraints", "set environment constraints")
	f.BoolVar(&c.UploadTools, "upload-tools", false, "upload local version of tools before bootstrapping")
	f.Var(seriesVar{&c.Series}, "series", "upload tools for supplied comma-separated series list")
}

func (c *BootstrapCommand) Init(args []string) error {
	if len(c.Series) > 0 && !c.UploadTools {
		return fmt.Errorf("--series requires --upload-tools")
	}
	return cmd.CheckEmpty(args)
}

// Run connects to the environment specified on the command line and bootstraps
// a juju in that environment if none already exists. If there is as yet no environments.yaml file,
// the user is informed how to create one.
func (c *BootstrapCommand) Run(context *cmd.Context) error {
	environ, err := environs.NewFromName(c.EnvName)
	if err != nil {
		if os.IsNotExist(err) {
			out := context.Stderr
			fmt.Fprintln(out, "No juju environment configuration file exists.")
			fmt.Fprintln(out, "Please create a configuration by running:")
			fmt.Fprintln(out, "    juju init -w")
			fmt.Fprintln(out, "then edit the file to configure your juju environment.")
			fmt.Fprintln(out, "You can then re-run bootstrap.")
		}
		return err
	}
	// TODO: if in verbose mode, write out to Stdout if a new cert was created.
	_, err = environs.EnsureCertificate(environ, environs.WriteCertAndKeyToHome)
	if err != nil {
		return err
	}

	if c.UploadTools {
		// Force version.Current, for consistency with subsequent upgrade-juju
		// (see UpgradeJujuCommand).
		forceVersion := version.Current.Number
		cfg := environ.Config()
		series := getUploadSeries(cfg, c.Series)
		tools, err := uploadTools(environ.Storage(), &forceVersion, series...)
		if err != nil {
			return err
		}
		cfg, err = cfg.Apply(map[string]interface{}{
			"agent-version": tools.Number.String(),
		})
		if err == nil {
			err = environ.SetConfig(cfg)
		}
		if err != nil {
			return fmt.Errorf("failed to update environment configuration: %v", err)
		}
	}
	return environs.Bootstrap(environ, c.Constraints)
}

type seriesVar struct {
	target *[]string
}

func (v seriesVar) Set(value string) error {
	names := strings.Split(value, ",")
	for _, name := range names {
		if !charm.IsValidSeries(name) {
			return fmt.Errorf("invalid series name %q", name)
		}
	}
	*v.target = names
	return nil
}

func (v seriesVar) String() string {
	return strings.Join(*v.target, ",")
}

// getUploadSeries returns the supplied series with duplicates removed if
// non-empty; otherwise it returns a default list of series we should
// probably upload, based on cfg.
func getUploadSeries(cfg *config.Config, series []string) []string {
	set := map[string]bool{}
	for _, series := range series {
		set[series] = true
	}
	if len(series) == 0 {
		set[version.Current.Series] = true
		set[config.DefaultSeries] = true
		set[cfg.DefaultSeries()] = true
	}
	result := []string{}
	for series := range set {
		result = append(result, series)
	}
	return result
}
