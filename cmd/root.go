package cmd

import (
	"fmt"
	"os"

	"github.com/equinixmetal/firmware-syncer/internal/app"
	"github.com/spf13/cobra"
)

var (
	debug    bool
	trace    bool
	dryRun   bool
	cfgFile  string
	logLevel int // 0 - info, 1 - debug, 2 - trace
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "firmware-syncer",
	Short: "Firmware syncer syncs firmware files from vendor repositories",
	Run: func(cmd *cobra.Command, args []string) {
		syncerApp := app.New(cfgFile, logLevel)
		syncerApp.SyncFirmwares(cmd.Context(), dryRun)
	},
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

func setLogLevel() {
	logLevel = app.LogLevelInfo

	if debug {
		logLevel = app.LogLevelDebug
	}

	if trace {
		logLevel = app.LogLevelTrace
	}
}

func init() {
	cobra.OnInitialize(setLogLevel)
	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "Enable debug logging (can be used with --trace)")
	rootCmd.PersistentFlags().BoolVarP(&trace, "trace", "t", false, "Enable trace logging (can be used with --debug)")
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config-file", "c", "", "Syncer configuration file")
	rootCmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "", false, "Don't sync anything, just initialize")
}
