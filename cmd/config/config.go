// Package config 实现 sec config：查看和修改 CLI 与 Desktop 共用的配置 ~/.sec/config.json。
package config

import (
	"errors"
	"fmt"
	"sort"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

// keys 是可以修改的配置项。
var keys = map[string]struct {
	description string
	get         func(config.Config) string
	set         func(*config.Config, string) error
}{
	"sec-edgar-email": {
		"访问美国 SEC EDGAR 时提供的联系邮箱",
		func(c config.Config) string { return c.SECEmail },
		func(c *config.Config, value string) error {
			if err := config.ValidateEmail(value); err != nil {
				return err
			}
			c.SECEmail = value
			return nil
		},
	},
}

func keyNames() []string {
	names := make([]string, 0, len(keys))
	for name := range keys {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func NewConfigCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or change settings shared by the CLI and Desktop (~/.sec/config.json)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load(cmd)
			if err != nil {
				return err
			}
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				return utils.PrintJSON(cmd.OutOrStdout(), cfg)
			}
			path, _ := config.Path()
			fmt.Fprintf(cmd.OutOrStdout(), "配置文件：%s\n\n", path)
			for _, name := range keyNames() {
				fmt.Fprintf(cmd.OutOrStdout(), "%s = %s\t# %s\n", name, keys[name].get(cfg), keys[name].description)
			}
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "Output JSON")
	cmd.AddCommand(&cobra.Command{
		Use:       "get <key>",
		Short:     "Print one setting",
		Args:      cobra.ExactArgs(1),
		ValidArgs: keyNames(),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, ok := keys[args[0]]
			if !ok {
				return fmt.Errorf("没有配置项 %s，可选：%v", args[0], keyNames())
			}
			cfg, err := load(cmd)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), key.get(cfg))
			return nil
		},
	}, &cobra.Command{
		Use:       "set <key> <value>",
		Short:     "Change one setting, e.g. sec config set sec-edgar-email you@example.com",
		Args:      cobra.ExactArgs(2),
		ValidArgs: keyNames(),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, ok := keys[args[0]]
			if !ok {
				return fmt.Errorf("没有配置项 %s，可选：%v", args[0], keyNames())
			}
			var cfg config.Config
			if err := config.Update(cmd.Context(), func(c *config.Config) error {
				if err := key.set(c, args[1]); err != nil {
					return err
				}
				cfg = *c
				return nil
			}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s = %s\n", args[0], key.get(cfg))
			return nil
		},
	})
	return cmd
}

// load 读取配置；文件内容无效时提示并使用默认值，可以用 sec config set 覆盖。
func load(cmd *cobra.Command) (config.Config, error) {
	cfg, err := config.Load(cmd.Context())
	if errors.Is(err, config.ErrInvalid) {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return cfg, nil
	}
	return cfg, err
}
