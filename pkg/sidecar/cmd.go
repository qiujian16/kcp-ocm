package sidecar

import (
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/qiujian16/kcp-ocm/pkg/version"
	"github.com/spf13/cobra"
)

func NewSidecar() *cobra.Command {
	sidecarOptions := &SidecarOptions{}
	cmdConfig := controllercmd.
		NewControllerCommandConfig("kcp-sidecar", version.Get(), sidecarOptions.Run)

	cmd := cmdConfig.NewCommand()
	cmd.Use = "sidecar"
	cmd.Short = "Start the sidecar for KCP"

	flags := cmd.Flags()
	sidecarOptions.AddFlags(flags)

	flags.BoolVar(&cmdConfig.DisableLeaderElection, "disable-leader-election", false, "Disable leader election for the sidecar.")

	return cmd
}
