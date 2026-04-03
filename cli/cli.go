package cli

import (
	"fmt"
	"github.com/kinwyb/kanflux/cli/cmd"
	"os"

	"github.com/spf13/cobra"
)

// Execute 执行CLI命令
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// NewRootCmd 创建根命令
func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "claw",
		Short: "ClawEino AI Agent CLI",
		Long:  `ClawEino - 一个基于Eino框架的AI Agent CLI工具`,
	}

	// 添加子命令
	rootCmd.AddCommand(cmd.NewTUICmd())

	return rootCmd
}
