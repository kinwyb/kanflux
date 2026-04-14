package cli

import (
	"fmt"
	"os"

	"github.com/kinwyb/kanflux/cli/cmd"

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
		Use:   "kanflux",
		Short: "KanFlux AI Agent CLI",
		Long:  `KanFlux - 一个基于Eino框架的AI Agent CLI工具`,
	}

	// 添加子命令
	rootCmd.AddCommand(cmd.NewTUICmd())
	rootCmd.AddCommand(cmd.NewConfigCmd())
	rootCmd.AddCommand(cmd.NewAgentCmd())
	rootCmd.AddCommand(cmd.NewRAGCmd())
	rootCmd.AddCommand(cmd.NewHistoryCmd())
	rootCmd.AddCommand(cmd.NewMemoryCmd())
	rootCmd.AddCommand(cmd.NewGatewayCmd())

	return rootCmd
}
