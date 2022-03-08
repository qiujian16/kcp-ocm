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
	cmd := controllercmd.
		NewControllerCommandConfig("kcp-manager", version.Get(), o.RunManager).
		NewCommand()
	cmd.Use = "manager"
	cmd.Short = "Start the KCP OCM integration manager"

	o.AddFlags(cmd)
	return cmd
}
