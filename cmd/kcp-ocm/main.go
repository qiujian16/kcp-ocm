package main

import (
	goflag "flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	utilflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"

	ocmcmd "github.com/qiujian16/kcp-ocm/pkg/cmd"
	"github.com/qiujian16/kcp-ocm/pkg/sidecar"
	"github.com/qiujian16/kcp-ocm/pkg/version"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	logs.InitLogs()
	defer logs.FlushLogs()

	command := newOCMCommand()
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func newOCMCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kcp-ocm",
		Short: "kcp ocm integration",
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
			os.Exit(1)
		},
	}

	if v := version.Get().String(); len(v) == 0 {
		cmd.Version = "<unknown>"
	} else {
		cmd.Version = v
	}

	cmd.AddCommand(ocmcmd.NewManager())
	cmd.AddCommand(sidecar.NewSidecar())

	return cmd
}
