package cmd

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/qiujian16/kcp-ocm/pkg/controllers"
	"github.com/qiujian16/kcp-ocm/pkg/version"
)

// NewWorkloadAgent generates a command to start workload agent
func NewManager() *cobra.Command {
	o := controllers.NewOCMManagerOptions()
	cmdConfig := controllercmd.
		NewControllerCommandConfig("kcp-manager", version.Get(), o.RunManager)
	cmd := cmdConfig.NewCommand()
	cmd.Use = "manager"
	cmd.Short = "Start the KCP OCM integration manager"

	flags := cmd.Flags()
	o.AddFlags(flags)
	flags.BoolVar(&cmdConfig.DisableLeaderElection, "disable-leader-election", false, "Disable leader election for the controller.")

	return cmd
}
