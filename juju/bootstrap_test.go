package main

import (
	"bytes"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"launchpad.net/juju-core/cmd"
	"launchpad.net/juju-core/environs"
	"launchpad.net/juju-core/environs/agent"
	"launchpad.net/juju-core/environs/dummy"
	"launchpad.net/juju-core/testing"
	"launchpad.net/juju-core/version"
	"net/http"
	"os"
	"strings"
)

type BootstrapSuite struct {
	testing.LoggingSuite
	testing.MgoSuite
}

var _ = Suite(&BootstrapSuite{})

func (s *BootstrapSuite) SetUpSuite(c *C) {
	s.LoggingSuite.SetUpSuite(c)
	s.MgoSuite.SetUpSuite(c)
}

func (s *BootstrapSuite) SetUpTest(c *C) {
	s.LoggingSuite.SetUpTest(c)
	s.MgoSuite.SetUpTest(c)
}

func (s *BootstrapSuite) TearDownSuite(c *C) {
	s.MgoSuite.TearDownSuite(c)
	s.LoggingSuite.TearDownSuite(c)
}

func (s *BootstrapSuite) TearDownTest(c *C) {
	s.MgoSuite.TearDownTest(c)
	s.LoggingSuite.TearDownTest(c)
	dummy.Reset()
}

func (*BootstrapSuite) TestBootstrapCommand(c *C) {
	defer makeFakeHome(c, "brokenenv").restore()
	err := ioutil.WriteFile(homePath(".juju", "environments.yaml"), []byte(envConfig), 0666)
	c.Assert(err, IsNil)

	// normal bootstrap
	opc, errc := runCommand(new(BootstrapCommand))
	c.Check(<-errc, IsNil)
	c.Check((<-opc).(dummy.OpBootstrap).Env, Equals, "peckham")

	// Check that the CA certificate and key have been automatically generated
	// for the environment.
	_, err = os.Stat(homePath(".juju", "peckham-cert.pem"))
	c.Assert(err, IsNil)
	_, err = os.Stat(homePath(".juju", "peckham-private-key.pem"))
	c.Assert(err, IsNil)

	// bootstrap with tool uploading - checking that a file
	// is uploaded should be sufficient, as the detailed semantics
	// of UploadTools are tested in environs.
	opc, errc = runCommand(new(BootstrapCommand), "--upload-tools")
	c.Check(<-errc, IsNil)
	c.Check((<-opc).(dummy.OpPutFile).Env, Equals, "peckham")
	c.Check((<-opc).(dummy.OpBootstrap).Env, Equals, "peckham")

	envs, err := environs.ReadEnvirons("")
	c.Assert(err, IsNil)
	env, err := envs.Open("peckham")
	c.Assert(err, IsNil)

	tools, err := environs.FindTools(env, version.Current, environs.CompatVersion)
	c.Assert(err, IsNil)
	resp, err := http.Get(tools.URL)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	err = agent.UnpackTools(c.MkDir(), tools, resp.Body)
	c.Assert(err, IsNil)

	// bootstrap with broken environment
	opc, errc = runCommand(new(BootstrapCommand), "-e", "brokenenv")
	c.Check(<-errc, ErrorMatches, "dummy.Bootstrap is broken")
	c.Check(<-opc, IsNil)
}

func (*BootstrapSuite) TestMissingEnvironment(c *C) {
	defer makeFakeHome(c, "empty").restore()
	// bootstrap without an environments.yaml
	ctx := testing.Context(c)
	code := cmd.Main(&BootstrapCommand{}, ctx, nil)
	c.Check(code, Equals, 1)
	errStr := ctx.Stderr.(*bytes.Buffer).String()
	strippedErr := strings.Replace(errStr, "\n", "", -1)
	c.Assert(strippedErr, Matches, ".*No juju environment configuration file exists.*")
}
