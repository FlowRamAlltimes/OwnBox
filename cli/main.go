package main

import (
	"fmt"
	"os"
	"ownbox-cli/app"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ownbox",
	Short: "Starts the server",
}

var getCmd = &cobra.Command{
	Use:   "get [hash] [output]",
	Short: "Here you can download data from OwnBox",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		hash := args[0]
		output := args[1]

		if err := app.Download(output, hash); err != nil {
			return err
		}
		fmt.Printf("Succesfully downloaded in %s\n", output)
		return nil
	},
}

var uploadCmd = &cobra.Command{
	Use:   "upload [filepath]",
	Short: "Uploads your file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filepath := args[0]

		if err := app.Upload(filepath); err != nil {
			fmt.Printf("Download failed. Log: %s", err.Error())
			return err
		}
		fmt.Printf("\n")
		fmt.Printf("Success!\n")
		return nil
	},
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "creates account",
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {

		if err := app.CreateAccount(); err != nil {
			return err
		}
		fmt.Printf("\n")
		fmt.Printf("Success!\n")

		return nil
	},
}

var removeItem = &cobra.Command{
	Use:   "rm [hash]",
	Short: "removes file by hash",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		hash := args[0]

		if err := app.DeleteItem(hash); err != nil {
			return err
		}

		fmt.Printf("Success!\n")

		return nil
	},
}

var removeAccount = &cobra.Command{
	Use:   "rmacc",
	Short: "removes account",
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {

		if err := app.DeleteAccount(); err != nil {
			return err
		}

		fmt.Printf("Success!\n")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(getCmd, uploadCmd, loginCmd, removeItem, removeAccount)
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	os.Exit(0)
}
