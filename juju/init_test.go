package main

import (
	"bytes"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"launchpad.net/juju-core/cmd"
	"launchpad.net/juju-core/testing"
	"strings"
)

type InitSuite struct {
}

var _ = Suite(&InitSuite{})

func (*InitSuite) TestBoilerPlateEnvironment(c *C) {
	defer testing.MakeEmptyFakeHome(c).Restore()
	// run without an environments.yaml
	ctx := testing.Context(c)
	code := cmd.Main(&InitCommand{}, ctx, []string{"-w"})
	c.Check(code, Equals, 0)
	outStr := ctx.Stdout.(*bytes.Buffer).String()
	strippedOut := strings.Replace(outStr, "\n", "", -1)
	c.Check(strippedOut, Matches, ".*A boilerplate environment configuration file has been written.*")
	environpath := testing.HomePath(".juju", "environments.yaml")
	data, err := ioutil.ReadFile(environpath)
	c.Assert(err, IsNil)
	strippedData := strings.Replace(string(data), "\n", "", -1)
	c.Assert(strippedData, Matches, ".*## This is the Juju config file, which you can use.*")
}

const existingEnv = `
environments:
    test:
        type: dummy
        state-server: false
        authorized-keys: i-am-a-key
`

func (*InitSuite) TestExistingEnvironmentNotOverwritten(c *C) {
	defer testing.MakeFakeHome(c, existingEnv, "existing").Restore()

	ctx := testing.Context(c)
	code := cmd.Main(&InitCommand{}, ctx, []string{"-w"})
	c.Check(code, Equals, 0)
	errOut := ctx.Stdout.(*bytes.Buffer).String()
	strippedOut := strings.Replace(errOut, "\n", "", -1)
	c.Check(strippedOut, Matches, ".*A juju environment configuration already exists.*")
	environpath := testing.HomePath(".juju", "environments.yaml")
	data, err := ioutil.ReadFile(environpath)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, existingEnv)
}

// Without the write (-w) option, any existing environmens.yaml file is preserved and the boilerplate is
// written to stdout.
func (*InitSuite) TestPrintBoilerplate(c *C) {
	defer testing.MakeFakeHome(c, existingEnv, "existing").Restore()

	ctx := testing.Context(c)
	code := cmd.Main(&InitCommand{}, ctx, nil)
	c.Check(code, Equals, 0)
	errOut := ctx.Stdout.(*bytes.Buffer).String()
	strippedOut := strings.Replace(errOut, "\n", "", -1)
	c.Check(strippedOut, Matches, ".*## This is the Juju config file, which you can use.*")
	environpath := testing.HomePath(".juju", "environments.yaml")
	data, err := ioutil.ReadFile(environpath)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, existingEnv)
}
