package cmd_test

import (
	"io/ioutil"
	. "launchpad.net/gocheck"
	"launchpad.net/juju-core/cmd"
	"launchpad.net/juju-core/log"
	"launchpad.net/juju-core/testing"
	"path/filepath"
)

type LogSuite struct {
	restoreLog func()
}

var _ = Suite(&LogSuite{})

func (s *LogSuite) SetUpTest(c *C) {
	target, debug := log.Target, log.Debug
	s.restoreLog = func() {
		log.Target, log.Debug = target, debug
	}
}

func (s *LogSuite) TearDownTest(c *C) {
	s.restoreLog()
}

func (s *LogSuite) TestAddFlags(c *C) {
	l := &cmd.Log{}
	f := testing.NewFlagSet()
	l.AddFlags(f)

	err := f.Parse(false, []string{})
	c.Assert(err, IsNil)
	c.Assert(l.Path, Equals, "")
	c.Assert(l.Verbose, Equals, false)
	c.Assert(l.Debug, Equals, false)

	err = f.Parse(false, []string{"--log-file", "foo", "--verbose", "--debug"})
	c.Assert(err, IsNil)
	c.Assert(l.Path, Equals, "foo")
	c.Assert(l.Verbose, Equals, true)
	c.Assert(l.Debug, Equals, true)
}

func (s *LogSuite) TestStart(c *C) {
	for _, t := range []struct {
		path    string
		verbose bool
		debug   bool
		target  Checker
	}{
		{"", true, true, NotNil},
		{"", true, false, NotNil},
		{"", false, true, NotNil},
		{"", false, false, IsNil},
		{"foo", true, true, NotNil},
		{"foo", true, false, NotNil},
		{"foo", false, true, NotNil},
		{"foo", false, false, NotNil},
	} {
		l := &cmd.Log{Prefix: "test", Path: t.path, Verbose: t.verbose, Debug: t.debug}
		ctx := testing.Context(c)
		err := l.Start(ctx)
		c.Assert(err, IsNil)
		c.Assert(log.Target, t.target)
		c.Assert(log.Debug, Equals, t.debug)
	}
}

func (s *LogSuite) TestStderr(c *C) {
	l := &cmd.Log{Prefix: "test", Verbose: true}
	ctx := testing.Context(c)
	err := l.Start(ctx)
	c.Assert(err, IsNil)
	log.Infof("hello")
	c.Assert(bufferString(ctx.Stderr), Matches, `JUJU:test:.* INFO: hello\n`)
}

func (s *LogSuite) TestRelPathLog(c *C) {
	l := &cmd.Log{Prefix: "test", Path: "foo.log"}
	ctx := testing.Context(c)
	err := l.Start(ctx)
	c.Assert(err, IsNil)
	log.Infof("hello")
	c.Assert(bufferString(ctx.Stderr), Equals, "")
	content, err := ioutil.ReadFile(filepath.Join(ctx.Dir, "foo.log"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Matches, `JUJU:test:.* INFO: hello\n`)
}

func (s *LogSuite) TestAbsPathLog(c *C) {
	path := filepath.Join(c.MkDir(), "foo.log")
	l := &cmd.Log{Prefix: "test", Path: path}
	ctx := testing.Context(c)
	err := l.Start(ctx)
	c.Assert(err, IsNil)
	log.Infof("hello")
	c.Assert(bufferString(ctx.Stderr), Equals, "")
	content, err := ioutil.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(string(content), Matches, `JUJU:test:.* INFO: hello\n`)
}
