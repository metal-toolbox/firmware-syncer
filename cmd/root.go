package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgFile         string
	logLevel        string
	enableProfiling bool
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "firmware-syncer",
	Short: "Firmware syncer syncs firmware files from vendor repositories",
}

func NewRootCmd() *cobra.Command {
	return rootCmd
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config-file", "c", "", "Syncer configuration file")
	rootCmd.PersistentFlags().BoolVarP(&enableProfiling, "enable-pprof", "", false, "Enable profiling endpoint at: "+"http://localhost:9091")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "set logging level - debug, trace")
}
