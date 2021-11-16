package controllers

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
)

// OCMManagerOptions defines the flags for ocm manager
type OCMManagerOptions struct {
	KCPRootCAFile string
}

// NewWorkloadAgentOptions returns the flags with default value set
func NewOCMManagerOptions() *OCMManagerOptions {
	return &OCMManagerOptions{}
}

// AddFlags register and binds the default flags
func (o *OCMManagerOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	// This command only supports reading from config
	flags.StringVar(&o.KCPRootCAFile, "kcp-ca", o.KCPRootCAFile, "Location of kcp ca file to connect to kcp.")
}

// RunWorkloadAgent starts the controllers on agent to process work from hub.
func (o *OCMManagerOptions) RunManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	<-ctx.Done()
	return nil
}
