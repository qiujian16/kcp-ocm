package cmd

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/qiujian16/kcp-ocm/pkg/controllers"
	"github.com/qiujian16/kcp-ocm/pkg/version"
)

// NewManager generates a command to start kcp-ocm integration controller manager
func NewManager() *cobra.Command {
	o := controllers.NewManagerOptions()
	cmdConfig := controllercmd.NewControllerCommandConfig("kcp-ocm-controller-manager", version.Get(), o.Run)
	cmd := cmdConfig.NewCommand()
	cmd.Use = "manager"
	cmd.Short = "Start the KCP OCM integration controller manager"

	flags := cmd.Flags()
	o.AddFlags(flags)
	flags.BoolVar(&cmdConfig.DisableLeaderElection, "disable-leader-election", false, "Disable leader election for the controller.")

	return cmd
}
