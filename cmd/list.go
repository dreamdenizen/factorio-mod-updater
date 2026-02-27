package cmd

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list [ROOT_DIR]",
	Short: "List the currently installed mods with versions",
	Run: func(cmd *cobra.Command, args []string) {
		updater, err := buildUpdater(args)
		if err != nil {
			pterm.Error.Println(err)
			return
		}

		if pterm.RawOutput {
			pterm.Info.Println("Starting Factorio Mod Updater (List Mode)...")
			pterm.Println("Fetching metadata and resolving dependencies...")
			err = updater.ResolveMetadata()
			if err != nil {
				pterm.Warning.Println("Some metadata could not be resolved:", err)
			}
			pterm.Success.Println("Metadata resolution complete")
		} else {
			spinner, _ := pterm.DefaultSpinner.Start("Fetching metadata and resolving dependencies...")
			err = updater.ResolveMetadata()
			if err != nil {
				spinner.Warning("Some metadata could not be resolved")
			} else {
				spinner.Success("Metadata fully resolved")
			}
		}

		mods := updater.GetMods()

		if pterm.RawOutput {
			// Non-TTY: only show mods needing attention + summary
			upToDate := 0
			outdated := 0
			missing := 0
			disabled := 0

			for _, mod := range mods {
				lver := "N/A"
				if mod.Latest != nil {
					lver = mod.Latest.Version
				}
				cver := "N/A"
				if mod.Installed {
					cver = mod.Version
				}

				if !mod.Enabled {
					disabled++
					fmt.Printf("  DISABLED  %s\n", mod.Title)
				} else if !mod.Installed {
					missing++
					fmt.Printf("  MISSING   %s (latest: %s)\n", mod.Title, lver)
				} else if cver != lver {
					outdated++
					fmt.Printf("  OUTDATED  %s (%s -> %s)\n", mod.Title, cver, lver)
				} else {
					upToDate++
				}
			}

			fmt.Printf("\nSummary: %d up to date, %d outdated, %d missing, %d disabled (%d total)\n",
				upToDate, outdated, missing, disabled, len(mods))
		} else {
			// TTY: full colorized table
			tableData := pterm.TableData{
				{"Mod Name", "Enabled", "Installed", "Current Version", "Latest Version"},
			}

			for _, mod := range mods {
				cver := "N/A"
				if mod.Installed {
					cver = mod.Version
				}
				lver := "N/A"
				if mod.Latest != nil {
					lver = mod.Latest.Version
				}

				titleStr := mod.Title
				cverStr := cver
				lverStr := lver

				if !mod.Installed || cver != lver {
					titleStr = pterm.Red(titleStr)
					cverStr = pterm.Red(cverStr)
					lverStr = pterm.Red(lverStr)
				} else {
					titleStr = pterm.Green(titleStr)
					cverStr = pterm.Green(cverStr)
					lverStr = pterm.Green(lverStr)
				}

				enabledStr := pterm.Red("false")
				if mod.Enabled {
					enabledStr = pterm.Green("true")
				}

				installedStr := pterm.Red("false")
				if mod.Installed {
					installedStr = pterm.Green("true")
				}

				tableData = append(tableData, []string{
					titleStr,
					enabledStr,
					installedStr,
					cverStr,
					lverStr,
				})
			}

			_ = pterm.DefaultTable.WithHasHeader().WithData(tableData).Render()
		}
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
