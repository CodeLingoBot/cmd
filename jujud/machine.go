package main

import (
	"fmt"
	"launchpad.net/gnuflag"
	"launchpad.net/juju-core/cmd"
	_ "launchpad.net/juju-core/environs/ec2"
	"launchpad.net/juju-core/log"
	"launchpad.net/juju-core/state"
	"launchpad.net/juju-core/worker"
	"launchpad.net/juju-core/worker/deployer"
	"launchpad.net/juju-core/worker/firewaller"
	"launchpad.net/juju-core/worker/provisioner"
	"launchpad.net/tomb"
	"time"
)

var retryDelay = 3 * time.Second

// MachineAgent is a cmd.Command responsible for running a machine agent.
type MachineAgent struct {
	tomb      tomb.Tomb
	Conf      AgentConf
	MachineId string
}

// Info returns usage information for the command.
func (a *MachineAgent) Info() *cmd.Info {
	return &cmd.Info{"machine", "", "run a juju machine agent", ""}
}

// Init initializes the command for running.
func (a *MachineAgent) Init(f *gnuflag.FlagSet, args []string) error {
	a.Conf.addFlags(f)
	f.StringVar(&a.MachineId, "machine-id", "", "id of the machine to run")
	if err := f.Parse(true, args); err != nil {
		return err
	}
	if !state.IsMachineId(a.MachineId) {
		return fmt.Errorf("--machine-id option must be set, and expects a non-negative integer")
	}
	return a.Conf.checkArgs(f.Args())
}

// Stop stops the machine agent.
func (a *MachineAgent) Stop() error {
	a.tomb.Kill(nil)
	return a.tomb.Wait()
}

// Run runs a machine agent.
func (a *MachineAgent) Run(_ *cmd.Context) error {
	if err := a.Conf.Read(state.MachineEntityName(a.MachineId)); err != nil {
		return err
	}

	defer log.Printf("cmd/jujud: machine agent exiting")
	defer a.tomb.Done()
	for a.tomb.Err() == tomb.ErrStillAlive {
		log.Printf("cmd/jujud: machine agent starting")
		err := a.runOnce()
		if ug, ok := err.(*UpgradeReadyError); ok {
			if err = ug.ChangeAgentTools(); err == nil {
				// Return and let upstart deal with the restart.
				return ug
			}
		}
		if err == worker.ErrDead {
			log.Printf("cmd/jujud: machine is dead")
			return nil
		}
		if err == nil {
			log.Printf("cmd/jujud: workers died with no error")
		} else {
			log.Printf("cmd/jujud: %v", err)
		}
		select {
		case <-a.tomb.Dying():
			a.tomb.Kill(err)
		case <-time.After(retryDelay):
			log.Printf("cmd/jujud: rerunning machiner")
		}
	}
	return a.tomb.Err()
}

func (a *MachineAgent) runOnce() error {
	st, passwordChanged, err := a.Conf.OpenState()
	if err != nil {
		return err
	}
	defer st.Close()
	m, err := st.Machine(a.MachineId)
	if state.IsNotFound(err) || err == nil && m.Life() == state.Dead {
		return worker.ErrDead
	}
	if err != nil {
		return err
	}
	if passwordChanged != "" {
		if err := m.SetPassword(a.Conf.StateInfo.Password); err != nil {
			return err
		}
	}
	log.Printf("cmd/jujud: running jobs for machine agent: %v", m.Jobs())
	tasks := []task{NewUpgrader(st, m, a.Conf.DataDir)}
	for _, j := range m.Jobs() {
		switch j {
		case state.JobHostUnits:
			info := &state.Info{
				EntityName: m.EntityName(),
				Addrs:      st.Addrs(),
				CACert:     st.CACert(),
			}
			mgr := deployer.NewSimpleManager(info, a.Conf.DataDir)
			tasks = append(tasks,
				deployer.NewDeployer(st, mgr, m.WatchPrincipalUnits()))
		case state.JobManageEnviron:
			tasks = append(tasks,
				provisioner.NewProvisioner(st),
				firewaller.NewFirewaller(st))
		default:
			log.Printf("cmd/jujud: ignoring unknown job %q", j)
		}
	}
	return runTasks(a.tomb.Dying(), tasks...)
}
