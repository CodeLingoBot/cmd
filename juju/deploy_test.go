package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"launchpad.net/juju-core/charm"
	"launchpad.net/juju-core/cmd"
	"launchpad.net/juju-core/juju/testing"
	"launchpad.net/juju-core/state"
	coretesting "launchpad.net/juju-core/testing"
	"net/http"
	"os"
	"path/filepath"
	"sort"
)

// repoSuite acts as a JujuConnSuite but also sets up
// $JUJU_REPOSITORY to point to a local charm repository.
type repoSuite struct {
	testing.JujuConnSuite
	seriesPath string
	repoPath   string
}

func (s *repoSuite) SetUpTest(c *C) {
	s.JujuConnSuite.SetUpTest(c)
	// Change the environ's config to ensure we're using the one in state,
	// not the one in the local environments.yaml
	cfg, err := s.State.EnvironConfig()
	c.Assert(err, IsNil)
	cfg, err = cfg.Apply(map[string]interface{}{"default-series": "precise"})
	c.Assert(err, IsNil)
	err = s.State.SetEnvironConfig(cfg)
	c.Assert(err, IsNil)
	s.repoPath = os.Getenv("JUJU_REPOSITORY")
	repoPath := c.MkDir()
	os.Setenv("JUJU_REPOSITORY", repoPath)
	s.seriesPath = filepath.Join(repoPath, "precise")
	err = os.Mkdir(s.seriesPath, 0777)
	c.Assert(err, IsNil)
}

func (s *repoSuite) TearDownTest(c *C) {
	os.Setenv("JUJU_REPOSITORY", s.repoPath)
	s.JujuConnSuite.TearDownTest(c)
}

func (s *repoSuite) assertService(c *C, name string, expectCurl *charm.URL, unitCount, relCount int) (*state.Service, []*state.Relation) {
	svc, err := s.State.Service(name)
	c.Assert(err, IsNil)
	ch, _, err := svc.Charm()
	c.Assert(err, IsNil)
	c.Assert(ch.URL(), DeepEquals, expectCurl)
	s.assertCharmUploaded(c, expectCurl)
	units, err := svc.AllUnits()
	c.Assert(err, IsNil)
	c.Assert(units, HasLen, unitCount)
	s.assertUnitMachines(c, units)
	rels, err := svc.Relations()
	c.Assert(err, IsNil)
	c.Assert(rels, HasLen, relCount)
	return svc, rels
}

func (s *repoSuite) assertCharmUploaded(c *C, curl *charm.URL) {
	ch, err := s.State.Charm(curl)
	c.Assert(err, IsNil)
	url := ch.BundleURL()
	resp, err := http.Get(url.String())
	c.Assert(err, IsNil)
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	h := sha256.New()
	h.Write(body)
	digest := hex.EncodeToString(h.Sum(nil))
	c.Assert(ch.BundleSha256(), Equals, digest)
}

func (s *repoSuite) assertUnitMachines(c *C, units []*state.Unit) {
	expectUnitNames := []string{}
	for _, u := range units {
		expectUnitNames = append(expectUnitNames, u.Name())
	}
	sort.Strings(expectUnitNames)

	machines, err := s.State.AllMachines()
	c.Assert(err, IsNil)
	c.Assert(machines, HasLen, len(units))
	unitNames := []string{}
	for _, m := range machines {
		mUnits, err := m.Units()
		c.Assert(err, IsNil)
		c.Assert(mUnits, HasLen, 1)
		unitNames = append(unitNames, mUnits[0].Name())
	}
	sort.Strings(unitNames)
	c.Assert(unitNames, DeepEquals, expectUnitNames)
}

type DeploySuite struct {
	repoSuite
}

var _ = Suite(&DeploySuite{})

func runDeploy(c *C, args ...string) error {
	com := &DeployCommand{}
	err := com.Init(newFlagSet(), args)
	c.Assert(err, IsNil)
	return com.Run(&cmd.Context{c.MkDir(), &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}})
}

var initErrorTests = []struct {
	args []string
	err  string
}{
	{
		args: nil,
		err:  `no charm specified`,
	}, {
		args: []string{"craz~ness"},
		err:  `invalid charm name "craz~ness"`,
	}, {
		args: []string{"craziness", "burble-1"},
		err:  `invalid service name "burble-1"`,
	}, {
		args: []string{"craziness", "burble1", "-n", "0"},
		err:  `must deploy at least one unit`,
	},
}

func (s *DeploySuite) TestInitErrors(c *C) {
	for i, t := range initErrorTests {
		c.Logf("test %d", i)
		com := &DeployCommand{}
		err := com.Init(newFlagSet(), t.args)
		c.Assert(err, ErrorMatches, t.err)
	}
}

func (s *DeploySuite) TestCharmDir(c *C) {
	coretesting.Charms.ClonedDirPath(s.seriesPath, "series", "dummy")
	err := runDeploy(c, "local:dummy")
	c.Assert(err, IsNil)
	curl := charm.MustParseURL("local:precise/dummy-1")
	s.assertService(c, "dummy", curl, 1, 0)
}

func (s *DeploySuite) TestUpgradeCharmDir(c *C) {
	dirPath := coretesting.Charms.ClonedDirPath(s.seriesPath, "series", "dummy")
	err := runDeploy(c, "local:dummy", "-u")
	c.Assert(err, IsNil)
	curl := charm.MustParseURL("local:precise/dummy-2")
	s.assertService(c, "dummy", curl, 1, 0)
	// Check the charm really was upgraded.
	ch, err := charm.ReadDir(dirPath)
	c.Assert(err, IsNil)
	c.Assert(ch.Revision(), Equals, 2)
}

func (s *DeploySuite) TestCharmBundle(c *C) {
	coretesting.Charms.BundlePath(s.seriesPath, "series", "dummy")
	err := runDeploy(c, "local:dummy", "some-service-name")
	c.Assert(err, IsNil)
	curl := charm.MustParseURL("local:precise/dummy-1")
	s.assertService(c, "some-service-name", curl, 1, 0)
}

func (s *DeploySuite) TestCannotUpgradeCharmBundle(c *C) {
	coretesting.Charms.BundlePath(s.seriesPath, "series", "dummy")
	err := runDeploy(c, "local:dummy", "-u")
	c.Assert(err, ErrorMatches, `cannot increment version of charm "local:precise/dummy-1": not a directory`)
	// Verify state not touched...
	curl := charm.MustParseURL("local:precise/dummy-1")
	_, err = s.State.Charm(curl)
	c.Assert(err, ErrorMatches, `charm "local:precise/dummy-1" not found`)
	_, err = s.State.Service("dummy")
	c.Assert(err, ErrorMatches, `service "dummy" not found`)
}

func (s *DeploySuite) TestAddsPeerRelations(c *C) {
	coretesting.Charms.BundlePath(s.seriesPath, "series", "riak")
	err := runDeploy(c, "local:riak")
	c.Assert(err, IsNil)
	curl := charm.MustParseURL("local:precise/riak-7")
	_, rels := s.assertService(c, "riak", curl, 1, 1)
	rel := rels[0]
	ep, err := rel.Endpoint("riak")
	c.Assert(err, IsNil)
	c.Assert(ep.RelationName, Equals, "ring")
	c.Assert(ep.RelationRole, Equals, state.RolePeer)
	c.Assert(ep.RelationScope, Equals, charm.ScopeGlobal)
}

func (s *DeploySuite) TestNumUnits(c *C) {
	coretesting.Charms.BundlePath(s.seriesPath, "series", "dummy")
	err := runDeploy(c, "local:dummy", "-n", "13")
	c.Assert(err, IsNil)
	curl := charm.MustParseURL("local:precise/dummy-1")
	s.assertService(c, "dummy", curl, 13, 0)
}

func (s *DeploySuite) TestSubordinateCharm(c *C) {
	coretesting.Charms.BundlePath(s.seriesPath, "series", "logging")
	err := runDeploy(c, "local:logging")
	c.Assert(err, IsNil)
	curl := charm.MustParseURL("local:precise/logging-1")
	s.assertService(c, "logging", curl, 0, 0)
}
