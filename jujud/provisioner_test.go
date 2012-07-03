package main

import (
	"time"

	. "launchpad.net/gocheck"
	"launchpad.net/juju-core/environs"
	"launchpad.net/juju-core/environs/dummy"
	"launchpad.net/juju-core/state"
	"launchpad.net/juju-core/testing"
	"strings"
)

type ProvisionerSuite struct {
	logging testing.LoggingSuite
	zkSuite
	st *state.State
	op <-chan dummy.Operation
}

var _ = Suite(&ProvisionerSuite{})

var veryShortAttempt = environs.AttemptStrategy{
	Total: 1 * time.Second,
	Delay: 80 * time.Millisecond,
}

func (s *ProvisionerSuite) SetUpTest(c *C) {
	s.logging.SetUpTest(c)

	// Create the operations channel with more than enough space
	// for those tests that don't listen on it.
	op := make(chan dummy.Operation, 500)
	dummy.Listen(op)
	s.op = op

	env, err := environs.NewEnviron(map[string]interface{}{
		"type":      "dummy",
		"zookeeper": true,
		"name":      "testing",
	})
	c.Assert(err, IsNil)
	err = env.Bootstrap(false)
	c.Assert(err, IsNil)

	s.zkSuite.SetUpTest(c)
	s.st, err = state.Open(s.zkInfo)
	c.Assert(err, IsNil)

	// Make sure that zkInfo holds exactly the info we're passing to
	// NewProvisioner,
	s.zkInfo, err = env.StateInfo()
	c.Assert(err, IsNil)
}

func (s *ProvisionerSuite) TearDownTest(c *C) {
	dummy.Reset()
	s.zkSuite.TearDownTest()
	s.logging.TearDownTest(c)
}

// invalidateEnvironment alters the environment configuration
// so the ConfigNode returned from the watcher will not pass
// validation.
func (s *ProvisionerSuite) invalidateEnvironment() error {
	env, err := s.st.EnvironConfig()
	if err != nil {
		return err
	}
	env.Set("name", 1)
	_, err = env.Write()
	return err
}

// fixEnvironment undoes the work of invalidateEnvironment.
func (s *ProvisionerSuite) fixEnvironment() error {
	env, err := s.st.EnvironConfig()
	if err != nil {
		return err
	}
	env.Set("name", "testing")
	_, err = env.Write()
	return err
}

func (s *ProvisionerSuite) stopProvisioner(c *C, p *Provisioner) {
	c.Assert(p.Stop(), IsNil)
}

// checkStartInstance checks that an instance has been started
// with a machine id the same as m's, and that the machine's
// instance id has been set appropriately.
func (s *ProvisionerSuite) checkStartInstance(c *C, m *state.Machine) {
	for {
		select {
		case o := <-s.op:
			switch o := o.(type) {
			case dummy.OpStartInstance:
				c.Assert(o.Info, DeepEquals, s.zkInfo)
				c.Assert(o.MachineId, Equals, m.Id())
				c.Assert(o.Instance, NotNil)
				s.checkMachineId(c, m, o.Instance)
				return
			default:
				c.Logf("ignoring unexpected operation %#v", o)
			}
		case <-time.After(2 * time.Second):
			c.Errorf("provisioner did not start an instance")
			return
		}
	}
}

// checkNotStartInstance checks that an instance was not started
func (s *ProvisionerSuite) checkNotStartInstance(c *C) {
	for {
		select {
		case o := <-s.op:
			switch o.(type) {
			case dummy.OpStartInstance:
				c.Errorf("instance started: %v", o)
				return
			default:
				// ignore   
			}
		case <-time.After(200 * time.Millisecond):
			return
		}
	}
}

// checkStopInstance checks that an instance has been stopped.
func (s *ProvisionerSuite) checkStopInstance(c *C) {
	// use the non fatal variants to avoid leaking provisioners.    
	for {
		select {
		case o := <-s.op:
			switch o.(type) {
			case dummy.OpStopInstances:
				return
			default:
				//ignore 
			}
		case <-time.After(2 * time.Second):
			c.Errorf("provisioner did not stop an instance")
			return
		}
	}
}

// checkMachineIdSet checks that the machine has an instance id
// that matches that of the given instance. If the instance is nil,
// It checks that the instance id is unset.
func (s *ProvisionerSuite) checkMachineId(c *C, m *state.Machine, inst environs.Instance) {
	// TODO(dfc) add machine.WatchConfig() to avoid having to poll.
	instId := ""
	if inst != nil {
		instId = inst.Id()
	}
	for a := veryShortAttempt.Start(); a.Next(); {
		_, err := m.InstanceId()
		_, notset := err.(*state.NoInstanceIdError)
		if notset {
			if inst == nil {
				return
			} else {
				continue
			}
		}
		c.Assert(err, IsNil)
		break
	}
	id, err := m.InstanceId()
	c.Assert(err, IsNil)
	c.Assert(id, Equals, instId)
}

func (s *ProvisionerSuite) TestProvisionerStartStop(c *C) {
	p, err := NewProvisioner(s.zkInfo)
	c.Assert(err, IsNil)
	c.Assert(p.Stop(), IsNil)
}

func (s *ProvisionerSuite) TestProvisionerDifferentStateInfo(c *C) {
	info := *s.zkInfo

	// Use an equivalent but textually different address and check
	// that the info when new instances are started is that returned
	// from the environment, not the one passed in originally.
	info.Addrs = append([]string{}, info.Addrs...)
	if !strings.HasPrefix(info.Addrs[0], "127.0.0.1") {
		c.Fatalf("local address %q not as expected", info.Addrs)
	}
	info.Addrs[0] = "localhost" + info.Addrs[0][len("127.0.0.1"):]

	p, err := NewProvisioner(&info)
	c.Assert(err, IsNil)
	defer s.stopProvisioner(c, p)

	// place a new machine into the state
	m, err := s.st.AddMachine()
	c.Assert(err, IsNil)

	s.checkStartInstance(c, m)
}

func (s *ProvisionerSuite) TestProvisionerEnvironmentChange(c *C) {
	p, err := NewProvisioner(s.zkInfo)
	c.Assert(err, IsNil)
	defer s.stopProvisioner(c, p)
	// Twiddle with the environment configuration.
	env, err := s.st.EnvironConfig()
	c.Assert(err, IsNil)
	env.Set("name", "testing2")
	_, err = env.Write()
	c.Assert(err, IsNil)
	env.Set("name", "testing3")
	_, err = env.Write()
}

func (s *ProvisionerSuite) TestProvisionerStopOnStateClose(c *C) {
	p, err := NewProvisioner(s.zkInfo)
	c.Assert(err, IsNil)

	p.st.Close()

	// must use Check to avoid leaking PA
	c.Check(p.Wait(), ErrorMatches, ".* zookeeper is closing")
	c.Assert(p.Stop(), ErrorMatches, ".* zookeeper is closing")
}

// Start and stop one machine, watch the PA.
func (s *ProvisionerSuite) TestSimple(c *C) {
	p, err := NewProvisioner(s.zkInfo)
	c.Assert(err, IsNil)
	defer s.stopProvisioner(c, p)

	// place a new machine into the state
	m, err := s.st.AddMachine()
	c.Assert(err, IsNil)

	s.checkStartInstance(c, m)

	// now remove it
	c.Assert(s.st.RemoveMachine(m.Id()), IsNil)

	// watch the PA remove it
	s.checkStopInstance(c)
	s.checkMachineId(c, m, nil)
}

func (s *ProvisionerSuite) TestProvisioningDoesNotOccurWithAnInvalidEnvironment(c *C) {
	err := s.invalidateEnvironment()
	c.Assert(err, IsNil)

	p, err := NewProvisioner(s.zkInfo)
	c.Assert(err, IsNil)
	defer s.stopProvisioner(c, p)

	// try to create a machine
	_, err = s.st.AddMachine()
	c.Assert(err, IsNil)

	// the PA should not create it
	s.checkNotStartInstance(c)
}

func (s *ProvisionerSuite) TestProvisioningOccursWithFixedEnvironment(c *C) {
	err := s.invalidateEnvironment()
	c.Assert(err, IsNil)

	p, err := NewProvisioner(s.zkInfo)
	c.Assert(err, IsNil)
	defer s.stopProvisioner(c, p)

	// try to create a machine
	m, err := s.st.AddMachine()
	c.Assert(err, IsNil)

	// the PA should not create it
	s.checkNotStartInstance(c)

	err = s.fixEnvironment()
	c.Assert(err, IsNil)

	s.checkStartInstance(c, m)
}

func (s *ProvisionerSuite) TestProvisioningDoesOccurAfterInvalidEnvironmentPublished(c *C) {
	p, err := NewProvisioner(s.zkInfo)
	c.Assert(err, IsNil)
	defer s.stopProvisioner(c, p)

	// place a new machine into the state
	m, err := s.st.AddMachine()
	c.Assert(err, IsNil)

	s.checkStartInstance(c, m)

	err = s.invalidateEnvironment()
	c.Assert(err, IsNil)

	// create a second machine
	m, err = s.st.AddMachine()
	c.Assert(err, IsNil)

	// the PA should create it using the old environment
	s.checkStartInstance(c, m)
}

func (s *ProvisionerSuite) TestProvisioningDoesNotProvisionTheSameMachineAfterRestart(c *C) {
	p, err := NewProvisioner(s.zkInfo)
	c.Check(err, IsNil)
	// we are not using defer s.stopProvisioner(c, p) because we need to control when 
	// the PA is restarted in this test. tf. Methods like Fatalf and Assert should not be used.

	// create a machine
	m, err := s.st.AddMachine()
	c.Check(err, IsNil)

	s.checkStartInstance(c, m)

	// restart the PA
	c.Check(p.Stop(), IsNil)

	p, err = NewProvisioner(s.zkInfo)
	c.Check(err, IsNil)

	// check that there is only one machine known
	machines, err := p.st.AllMachines()
	c.Check(err, IsNil)
	c.Check(len(machines), Equals, 1)
	c.Check(machines[0].Id(), Equals, 0)

	// the PA should not create it a second time
	s.checkNotStartInstance(c)

	c.Assert(p.Stop(), IsNil)
}

func (s *ProvisionerSuite) TestProvisioningStopsUnknownInstances(c *C) {
	p, err := NewProvisioner(s.zkInfo)
	c.Check(err, IsNil)
	// we are not using defer s.stopProvisioner(c, p) because we need to control when 
	// the PA is restarted in this test. Methods like Fatalf and Assert should not be used.

	// create a machine
	m, err := s.st.AddMachine()
	c.Check(err, IsNil)

	s.checkStartInstance(c, m)

	// create a second machine
	m, err = s.st.AddMachine()
	c.Check(err, IsNil)

	s.checkStartInstance(c, m)

	// stop the PA
	c.Check(p.Stop(), IsNil)

	// remove the machine
	err = s.st.RemoveMachine(m.Id())
	c.Check(err, IsNil)

	// start a new provisioner
	p, err = NewProvisioner(s.zkInfo)
	c.Check(err, IsNil)

	s.checkStopInstance(c)

	c.Assert(p.Stop(), IsNil)
}

// This check is different from the one above as it catches the edge case
// where the final machine has been removed from the state while the PA was 
// not running. 
func (s *ProvisionerSuite) TestProvisioningStopsOnlyUnknownInstances(c *C) {
	p, err := NewProvisioner(s.zkInfo)
	c.Check(err, IsNil)
	// we are not using defer s.stopProvisioner(c, p) because we need to control when 
	// the PA is restarted in this test. Methods like Fatalf and Assert should not be used.

	// create a machine
	m, err := s.st.AddMachine()
	c.Check(err, IsNil)

	s.checkStartInstance(c, m)

	// stop the PA
	c.Check(p.Stop(), IsNil)

	// remove the machine
	err = s.st.RemoveMachine(m.Id())
	c.Check(err, IsNil)

	machines, err := s.st.AllMachines()
	c.Check(err, IsNil)
	c.Check(len(machines), Equals, 0) // it's really gone   

	// start a new provisioner
	p, err = NewProvisioner(s.zkInfo)
	c.Check(err, IsNil)

	s.checkStopInstance(c)

	c.Assert(p.Stop(), IsNil)
}

func (s *ProvisionerSuite) TestProvisioningRecoversAfterInvalidEnvironmentPublished(c *C) {
	p, err := NewProvisioner(s.zkInfo)
	c.Assert(err, IsNil)
	defer s.stopProvisioner(c, p)

	// place a new machine into the state
	m, err := s.st.AddMachine()
	c.Assert(err, IsNil)

	s.checkStartInstance(c, m)

	err = s.invalidateEnvironment()
	c.Assert(err, IsNil)

	// create a second machine
	m, err = s.st.AddMachine()
	c.Assert(err, IsNil)

	// the PA should create it using the old environment
	s.checkStartInstance(c, m)

	err = s.fixEnvironment()
	c.Assert(err, IsNil)

	// create a third machine
	m, err = s.st.AddMachine()
	c.Assert(err, IsNil)

	// the PA should create it using the new environment
	s.checkStartInstance(c, m)
}
