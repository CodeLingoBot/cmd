package main

import (
	"fmt"
	. "launchpad.net/gocheck"
	"launchpad.net/juju-core/cmd"
	"launchpad.net/juju-core/environs/agent"
	"launchpad.net/juju-core/environs/dummy"
	"launchpad.net/juju-core/state"
	"launchpad.net/juju-core/state/api"
	"launchpad.net/juju-core/testing"
	"reflect"
	"time"
)

type MachineSuite struct {
	agentSuite
}

var _ = Suite(&MachineSuite{})

// primeAgent adds a new Machine to run the given jobs, and sets up the
// machine agent's directory.  It returns the new machine, the
// agent's configuration and the tools currently running.
func (s *MachineSuite) primeAgent(c *C, jobs ...state.MachineJob) (*state.Machine, *agent.Conf, *state.Tools) {
	m, err := s.State.InjectMachine("ardbeg-0", jobs...)
	c.Assert(err, IsNil)
	err = m.SetMongoPassword("machine-password")
	c.Assert(err, IsNil)
	conf, tools := s.agentSuite.primeAgent(c, state.MachineEntityName(m.Id()), "machine-password")
	return m, conf, tools
}

// newAgent returns a new MachineAgent instance
func (s *MachineSuite) newAgent(c *C, m *state.Machine) *MachineAgent {
	a := &MachineAgent{}
	s.initAgent(c, a, "--machine-id", m.Id())
	return a
}

func (s *MachineSuite) TestParseSuccess(c *C) {
	create := func() (cmd.Command, *AgentConf) {
		a := &MachineAgent{}
		return a, &a.Conf
	}
	a := CheckAgentCommand(c, create, []string{"--machine-id", "42"})
	c.Assert(a.(*MachineAgent).MachineId, Equals, "42")
}

func (s *MachineSuite) TestParseNonsense(c *C) {
	for _, args := range [][]string{
		{},
		{"--machine-id", "-4004"},
	} {
		err := ParseAgentCommand(&MachineAgent{}, args)
		c.Assert(err, ErrorMatches, "--machine-id option must be set, and expects a non-negative integer")
	}
}

func (s *MachineSuite) TestParseUnknown(c *C) {
	a := &MachineAgent{}
	err := ParseAgentCommand(a, []string{"--machine-id", "42", "blistering barnacles"})
	c.Assert(err, ErrorMatches, `unrecognized args: \["blistering barnacles"\]`)
}

func (s *MachineSuite) TestRunInvalidMachineId(c *C) {
	c.Skip("agents don't yet distinguish between temporary and permanent errors")
	m, _, _ := s.primeAgent(c, state.JobHostUnits)
	err := s.newAgent(c, m).Run(nil)
	c.Assert(err, ErrorMatches, "some error")
}

func (s *MachineSuite) TestRunStop(c *C) {
	m, _, _ := s.primeAgent(c, state.JobHostUnits)
	a := s.newAgent(c, m)
	done := make(chan error)
	go func() {
		done <- a.Run(nil)
	}()
	err := a.Stop()
	c.Assert(err, IsNil)
	c.Assert(<-done, IsNil)
}

func (s *MachineSuite) TestWithDeadMachine(c *C) {
	m, _, _ := s.primeAgent(c, state.JobHostUnits, state.JobServeAPI)
	err := m.EnsureDead()
	c.Assert(err, IsNil)
	a := s.newAgent(c, m)
	err = runWithTimeout(a)
	c.Assert(err, IsNil)

	// try again with the machine removed.
	err = m.Remove()
	c.Assert(err, IsNil)
	a = s.newAgent(c, m)
	err = runWithTimeout(a)
	c.Assert(err, IsNil)
}

func (s *MachineSuite) TestHostUnits(c *C) {
	m, conf, _ := s.primeAgent(c, state.JobHostUnits)
	a := s.newAgent(c, m)
	mgr, reset := patchDeployManager(c, conf.StateInfo, conf.DataDir)
	defer reset()
	go func() { c.Check(a.Run(nil), IsNil) }()
	defer func() { c.Check(a.Stop(), IsNil) }()

	svc, err := s.State.AddService("wordpress", s.AddTestingCharm(c, "wordpress"))
	c.Assert(err, IsNil)
	u0, err := svc.AddUnit()
	c.Assert(err, IsNil)
	u1, err := svc.AddUnit()
	c.Assert(err, IsNil)
	mgr.waitDeployed(c)

	err = u0.AssignToMachine(m)
	c.Assert(err, IsNil)
	mgr.waitDeployed(c, u0.Name())

	err = u0.Destroy()
	c.Assert(err, IsNil)
	mgr.waitDeployed(c, u0.Name())

	err = u1.AssignToMachine(m)
	c.Assert(err, IsNil)
	mgr.waitDeployed(c, u0.Name(), u1.Name())

	err = u0.EnsureDead()
	c.Assert(err, IsNil)
	mgr.waitDeployed(c, u1.Name())

	err = u0.Refresh()
	c.Assert(state.IsNotFound(err), Equals, true)
}

func (s *MachineSuite) TestManageEnviron(c *C) {
	m, _, _ := s.primeAgent(c, state.JobManageEnviron)
	op := make(chan dummy.Operation, 200)
	dummy.Listen(op)

	a := s.newAgent(c, m)
	// Make sure the agent is stopped even if the test fails.
	defer a.Stop()
	done := make(chan error)
	go func() {
		done <- a.Run(nil)
	}()

	// Check that the provisioner and firewaller are alive by doing
	// a rudimentary check that it responds to state changes.

	// Add one unit to a service; it should get allocated a machine
	// and then its ports should be opened.
	charm := s.AddTestingCharm(c, "dummy")
	svc, err := s.State.AddService("test-service", charm)
	c.Assert(err, IsNil)
	err = svc.SetExposed()
	c.Assert(err, IsNil)
	units, err := s.Conn.AddUnits(svc, 1)
	c.Assert(err, IsNil)
	c.Check(opRecvTimeout(c, s.State, op, dummy.OpStartInstance{}), NotNil)

	// Wait for the instance id to show up in the state.
	id1, err := units[0].AssignedMachineId()
	c.Assert(err, IsNil)
	m1, err := s.State.Machine(id1)
	c.Assert(err, IsNil)
	w := m1.Watch()
	defer w.Stop()
	for _ = range w.Changes() {
		err = m1.Refresh()
		c.Assert(err, IsNil)
		if _, ok := m1.InstanceId(); ok {
			break
		}
	}
	err = units[0].OpenPort("tcp", 999)
	c.Assert(err, IsNil)

	c.Check(opRecvTimeout(c, s.State, op, dummy.OpOpenPorts{}), NotNil)

	err = a.Stop()
	c.Assert(err, IsNil)

	select {
	case err := <-done:
		c.Assert(err, IsNil)
	case <-time.After(5 * time.Second):
		c.Fatalf("timed out waiting for agent to terminate")
	}
}

func (s *MachineSuite) TestUpgrade(c *C) {
	m, conf, currentTools := s.primeAgent(c, state.JobServeAPI, state.JobManageEnviron, state.JobHostUnits)
	addAPIInfo(conf, m)
	err := conf.Write()
	c.Assert(err, IsNil)
	a := s.newAgent(c, m)
	s.testUpgrade(c, a, currentTools)
}

func addAPIInfo(conf *agent.Conf, m *state.Machine) {
	port := testing.FindTCPPort()
	conf.APIInfo = &api.Info{
		Addrs:      []string{fmt.Sprintf("localhost:%d", port)},
		CACert:     []byte(testing.CACert),
		EntityName: m.EntityName(),
		Password:   "unused",
	}
	conf.StateServerCert = []byte(testing.ServerCert)
	conf.StateServerKey = []byte(testing.ServerKey)
	conf.APIPort = port
}

func (s *MachineSuite) TestServeAPI(c *C) {
	stm, conf, _ := s.primeAgent(c, state.JobServeAPI)
	addAPIInfo(conf, stm)
	err := conf.Write()
	c.Assert(err, IsNil)
	a := s.newAgent(c, stm)
	done := make(chan error)
	go func() {
		done <- a.Run(nil)
	}()

	st, err := api.Open(conf.APIInfo)
	c.Assert(err, IsNil)
	defer st.Close()

	m, err := st.Machine(stm.Id())
	c.Assert(err, IsNil)

	instId, ok := m.InstanceId()
	c.Assert(ok, Equals, true)
	c.Assert(instId, Equals, "ardbeg-0")

	err = a.Stop()
	c.Assert(err, IsNil)

	select {
	case err := <-done:
		c.Assert(err, IsNil)
	case <-time.After(5 * time.Second):
		c.Fatalf("timed out waiting for agent to terminate")
	}
}

var serveAPIWithBadConfTests = []struct {
	change func(c *agent.Conf)
	err    string
}{{
	func(c *agent.Conf) {
		c.StateServerCert = nil
	},
	"configuration does not have state server cert/key",
}, {
	func(c *agent.Conf) {
		c.StateServerKey = nil
	},
	"configuration does not have state server cert/key",
}}

func (s *MachineSuite) TestServeAPIWithBadConf(c *C) {
	m, conf, _ := s.primeAgent(c, state.JobServeAPI)
	addAPIInfo(conf, m)
	for i, t := range serveAPIWithBadConfTests {
		c.Logf("test %d: %q", i, t.err)
		conf1 := *conf
		t.change(&conf1)
		err := conf1.Write()
		c.Assert(err, IsNil)
		a := s.newAgent(c, m)
		err = runWithTimeout(a)
		c.Assert(err, ErrorMatches, t.err)
		err = refreshConfig(conf)
		c.Assert(err, IsNil)
	}
}

// opRecvTimeout waits for any of the given kinds of operation to
// be received from ops, and times out if not.
func opRecvTimeout(c *C, st *state.State, opc <-chan dummy.Operation, kinds ...dummy.Operation) dummy.Operation {
	st.StartSync()
	for {
		select {
		case op := <-opc:
			for _, k := range kinds {
				if reflect.TypeOf(op) == reflect.TypeOf(k) {
					return op
				}
			}
			c.Logf("discarding unknown event %#v", op)
		case <-time.After(5 * time.Second):
			c.Fatalf("time out wating for operation")
		}
	}
	panic("not reached")
}

func (s *MachineSuite) TestChangePasswordChanging(c *C) {
	m, _, _ := s.primeAgent(c, state.JobHostUnits)
	newAgent := func() runner {
		return s.newAgent(c, m)
	}
	s.testAgentPasswordChanging(c, m, newAgent)
}
