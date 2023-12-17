package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/metal-toolbox/firmware-syncer/internal/app"
	"github.com/metal-toolbox/firmware-syncer/pkg/types"
	"github.com/spf13/cobra"
)

var (
	cfgFile       string
	inventoryKind string
	logLevel      string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "firmware-syncer",
	Short: "Firmware syncer syncs firmware files from vendor repositories",
	Run: func(cmd *cobra.Command, args []string) {
		if cfgFile == "" {
			fmt.Println("No firmware-syncer configuration file found.")
			os.Exit(1)
		}

		syncerApp, err := app.New(cmd.Context(), types.InventoryKind(inventoryKind), cfgFile, logLevel)
		if err != nil {
			log.Fatal(err)
		}

		syncerApp.Logger.Info("Sync starting")
		err = syncerApp.SyncFirmwares(cmd.Context())
		if err != nil {
			syncerApp.Logger.Fatal(err)
		}
		syncerApp.Logger.Info("Sync complete")
	},
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
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "set logging level - info, debug, trace")
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config-file", "c", "", "Syncer configuration file")
	rootCmd.PersistentFlags().StringVar(&inventoryKind, "inventory", "serverservice", "Inventory to publish firmwares.")
}
