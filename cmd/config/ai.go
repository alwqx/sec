package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// newAICLI 返回 sec config ai：管理 config.json 的 ai 段里的模型配置，设计见 docs/ai.md。
func newAICLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ai",
		Short: "Manage model profiles for AI features, shared by the CLI and Desktop",
		Long: "Manage model profiles in the ai section of ~/.sec/config.json.\n" +
			"Each profile names a provider and a model; the API and base URL default to the provider's official ones.\n" +
			"Providers: " + providerList(),
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newAIListCLI(), newAIAddCLI(), newAIEditCLI(), newAISetKeyCLI(), newAIUnsetKeyCLI(), newAIUseCLI(), newAIRemoveCLI(), newAIEnableCLI(true), newAIEnableCLI(false))
	return cmd
}

func providerList() string { return strings.Join(config.AIProviderIDs(), ", ") }

// checkAI 在列出或修改模型配置前检查 ai 段。ai 段无效时读取会把它当成空的：list 会说没有配置、use 会说找不到、
// disable 什么也没改却说已关闭，所以直接报原因，要用户先手动修正。
func checkAI(cmd *cobra.Command) error {
	if _, err := config.Load(cmd.Context()); errors.Is(err, config.ErrInvalidAI) {
		return fmt.Errorf("%w；请先手动修正或删除 ai 段，再使用 sec config ai", err)
	}
	return nil
}

// remoteNotice 在 AI 已启用时提醒：这个配置不是本机模型，查询会发给外部厂商。第一次启用时的完整提示见 privacyNotice。
func remoteNotice(out io.Writer, ai config.AIConfig, name string) {
	p := ai.Profiles[name]
	if ai.Enabled && !p.IsLocal() {
		fmt.Fprintf(out, "注意：%s 不是本机模型，使用它时查询的证券、自选股和工具结果会发送给%s。\n", name, providerName(p.Provider))
	}
}

// aiProfileView 是 list --json 里的一个配置：协议和地址是实际使用的值，Key 只给来源和末 4 位。
type aiProfileView struct {
	Name         string `json:"name"`
	Default      bool   `json:"default"`
	Provider     string `json:"provider"`
	ProviderName string `json:"provider_name"`
	API          string `json:"api"`
	BaseURL      string `json:"base_url"`
	Model        string `json:"model"`
	APIKeyEnv    string `json:"api_key_env,omitempty"`
	Key          string `json:"key"`
	Local        bool   `json:"local"`
}

func newAIListCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List model profiles, marking the default; keys show only their source and last 4 characters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkAI(cmd); err != nil {
				return err
			}
			cfg, err := load(cmd)
			if err != nil {
				return err
			}
			secret, err := config.DefaultAISecret()
			if err != nil {
				return err
			}
			ai := cfg.AI
			views := []aiProfileView{}
			for _, name := range ai.Names() {
				p := ai.Profiles[name]
				provider, _ := config.LookupAIProvider(p.Provider)
				views = append(views, aiProfileView{
					Name: name, Default: name == ai.Default, Provider: p.Provider, ProviderName: provider.Name,
					API: p.EffectiveAPI(), BaseURL: p.EffectiveBaseURL(), Model: p.Model, APIKeyEnv: p.APIKeyEnv,
					Key: ai.KeyStatus(secret, name, os.Getenv), Local: p.IsLocal(),
				})
			}
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				return utils.PrintJSON(cmd.OutOrStdout(), map[string]any{"enabled": ai.Enabled, "default": ai.Default, "profiles": views})
			}
			out := cmd.OutOrStdout()
			if len(views) == 0 {
				_, err := fmt.Fprintln(out, "还没有模型配置。添加：sec config ai add ds --provider deepseek --model deepseek-chat")
				return err
			}
			state := "未启用，用 sec config ai enable 启用"
			if ai.Enabled {
				state = "已启用"
			}
			fmt.Fprintf(out, "AI 功能：%s\n\n", state)
			table := tablewriter.NewWriter(out)
			table.SetBorder(false)
			table.SetAutoWrapText(false)
			table.SetHeader([]string{"配置", "厂商", "协议", "地址", "模型", "Key"})
			for _, v := range views {
				name := v.Name
				if v.Default {
					name += " *"
				}
				table.Append([]string{name, v.ProviderName, v.API, v.BaseURL, v.Model, v.Key})
			}
			table.Render()
			_, err = fmt.Fprintln(out, "* 是 default，不指定配置时使用。")
			return err
		},
	}
	cmd.Flags().Bool("json", false, "Output JSON")
	return cmd
}

// profileFlags 是 add 和 edit 共用的参数。
func profileFlags(cmd *cobra.Command) {
	cmd.Flags().String("provider", "", "Provider: "+providerList())
	cmd.Flags().String("api", "", "API protocol, anthropic or openai; defaults to the provider's, required for custom")
	cmd.Flags().String("base-url", "", "Base URL; defaults to the provider's official one, required for custom")
	cmd.Flags().String("model", "", "Model, e.g. deepseek-chat")
	cmd.Flags().String("api-key-env", "", "Read the API key from this environment variable instead of the stored key")
	_ = cmd.RegisterFlagCompletionFunc("provider", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return config.AIProviderIDs(), cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("api", cobra.FixedCompletions(config.AIAPIs, cobra.ShellCompDirectiveNoFileComp))
}

// profileFlagNames 是 add 和 edit 共用的参数名。
var profileFlagNames = []string{"provider", "api", "base-url", "model", "api-key-env"}

// applyProfileFlags 把用户给出的参数写进配置，没给的不动；给空字符串表示清空（如 --base-url ""）。
func applyProfileFlags(cmd *cobra.Command, p *config.AIProfile) {
	for flag, field := range map[string]*string{"provider": &p.Provider, "api": &p.API, "base-url": &p.BaseURL, "model": &p.Model, "api-key-env": &p.APIKeyEnv} {
		if cmd.Flags().Changed(flag) {
			*field, _ = cmd.Flags().GetString(flag)
			*field = strings.TrimSpace(*field)
		}
	}
}

func newAIAddCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "add <name>",
		Short:   "Add a model profile; the first one becomes the default",
		Example: "  sec config ai add ds --provider deepseek --model deepseek-chat\n  sec config ai add gw --provider custom --api openai --base-url https://llm.example.com/v1 --model x",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkAI(cmd); err != nil {
				return err
			}
			name := args[0]
			var p config.AIProfile
			applyProfileFlags(cmd, &p)
			var ai config.AIConfig
			if err := config.Update(cmd.Context(), func(c *config.Config) error {
				if err := c.AI.Add(name, p); err != nil {
					return err
				}
				ai = c.AI
				return nil
			}); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "已添加模型配置 %s（%s，%s）\n", name, providerName(p.Provider), p.Model)
			if ai.Default == name {
				fmt.Fprintf(out, "%s 是 default。\n", name)
			}
			if env := p.DefaultKeyEnv(); env != "" && p.APIKeyEnv == "" {
				fmt.Fprintf(out, "Key 可以用环境变量 %s 提供，或用 --api-key-env 指定别的环境变量。\n", env)
			}
			if !ai.Enabled {
				fmt.Fprintln(out, "AI 功能还没有启用，用 sec config ai enable 启用。")
			}
			remoteNotice(out, ai, name)
			return nil
		},
	}
	profileFlags(cmd)
	_ = cmd.MarkFlagRequired("provider")
	_ = cmd.MarkFlagRequired("model")
	return cmd
}

func newAIEditCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "edit <name>",
		Short:   "Change a model profile; only the given flags change, an empty value clears an optional one",
		Example: "  sec config ai edit ds --model deepseek-reasoner\n  sec config ai edit ds --base-url \"\"",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !slices.ContainsFunc(profileFlagNames, cmd.Flags().Changed) {
				return fmt.Errorf("没有要修改的项，可以用 --provider、--api、--base-url、--model、--api-key-env")
			}
			if err := checkAI(cmd); err != nil {
				return err
			}
			name := args[0]
			var p config.AIProfile
			keyCleared := false
			if err := config.Update(cmd.Context(), func(c *config.Config) error {
				hadKey := c.AI.Profiles[name].APIKey != ""
				if err := c.AI.Edit(name, func(profile *config.AIProfile) { applyProfileFlags(cmd, profile) }); err != nil {
					return err
				}
				p = c.AI.Profiles[name]
				keyCleared = hadKey && p.APIKey == ""
				return nil
			}); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "已修改模型配置 %s：%s，%s，%s，%s\n", name, providerName(p.Provider), p.EffectiveAPI(), p.EffectiveBaseURL(), p.Model)
			if keyCleared {
				fmt.Fprintln(out, "厂商或地址变了，原来保存的 Key 不会发给新地址，已删除，请重新设置。")
			}
			return nil
		},
	}
	profileFlags(cmd)
	return cmd
}

func newAIUseCLI() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Make a model profile the default",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkAI(cmd); err != nil {
				return err
			}
			var ai config.AIConfig
			if err := config.Update(cmd.Context(), func(c *config.Config) error {
				if err := c.AI.Use(args[0]); err != nil {
					return err
				}
				ai = c.AI
				return nil
			}); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "default 已改为 %s\n", args[0])
			remoteNotice(out, ai, args[0])
			return nil
		},
	}
}

func newAIRemoveCLI() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a model profile and its stored key; the default can't be removed while others exist",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkAI(cmd); err != nil {
				return err
			}
			var ai config.AIConfig
			hadKey, wasEnabled := false, false
			if err := config.Update(cmd.Context(), func(c *config.Config) error {
				hadKey, wasEnabled = c.AI.Profiles[args[0]].APIKey != "", c.AI.Enabled
				if err := c.AI.Remove(args[0]); err != nil {
					return err
				}
				ai = c.AI
				return nil
			}); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if hadKey {
				fmt.Fprintf(out, "已删除模型配置 %s，保存的 Key 也一并删除\n", args[0])
			} else {
				fmt.Fprintf(out, "已删除模型配置 %s\n", args[0])
			}
			if len(ai.Profiles) == 0 && wasEnabled {
				fmt.Fprintln(out, "已经没有模型配置，AI 功能已关闭。")
			}
			return nil
		},
	}
}

func newAIEnableCLI(enable bool) *cobra.Command {
	use, short := "disable", "Turn off AI features"
	if enable {
		use, short = "enable", "Turn on AI features; queries are then sent to the chosen provider"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkAI(cmd); err != nil {
				return err
			}
			var ai config.AIConfig
			wasEnabled := false
			if err := config.Update(cmd.Context(), func(c *config.Config) error {
				wasEnabled = c.AI.Enabled
				if err := c.AI.SetEnabled(enable); err != nil {
					return err
				}
				ai = c.AI
				return nil
			}); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if !enable {
				_, err := fmt.Fprintln(out, "AI 功能已关闭")
				return err
			}
			fmt.Fprintf(out, "AI 功能已启用，default 是 %s\n", ai.Default)
			if !wasEnabled {
				return privacyNotice(out, ai)
			}
			return nil
		},
	}
}

// privacyNotice 是首次启用时的隐私提示，见 docs/ai.md：说明哪些数据会发给哪个厂商，本机模型的数据不出本机。
func privacyNotice(out io.Writer, ai config.AIConfig) error {
	remote, local := []string{}, []string{}
	for _, name := range ai.Names() {
		p := ai.Profiles[name]
		if p.IsLocal() {
			local = append(local, name)
		} else {
			remote = append(remote, fmt.Sprintf("%s（%s）", name, providerName(p.Provider)))
		}
	}
	fmt.Fprintln(out, "\n注意：使用 AI 功能时，查询的证券、自选股和工具结果会发送给所选配置的厂商，按厂商的隐私政策处理。")
	if len(remote) > 0 {
		fmt.Fprintf(out, "会发送到外部厂商的配置：%s\n", strings.Join(remote, "、"))
	}
	if len(local) > 0 {
		fmt.Fprintf(out, "本机模型，数据不出本机：%s\n", strings.Join(local, "、"))
	}
	_, err := fmt.Fprintln(out, "回答只解释数据，不构成投资建议。用 sec config ai disable 可以随时关闭。")
	return err
}

// maxKeyLength 是从 stdin 读 Key 时最多读的字节数，Key 都远短于此。
const maxKeyLength = 4096

func newAISetKeyCLI() *cobra.Command {
	return &cobra.Command{
		Use:   "set-key <name>",
		Short: "Store a profile's API key, encrypted; typed hidden in a terminal or read as one line from stdin, never from arguments",
		Long: "Store a profile's API key encrypted in ~/.sec/config.json, with the encryption key in ~/.sec/secret.key.\n" +
			"In a terminal the key is typed without echo; otherwise one line is read from stdin, e.g. pbpaste | sec config ai set-key ds.\n" +
			"The key is never taken as an argument, so it stays out of shell history and the process list.\n" +
			"Don't sync or back up secret.key together with config.json.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			// 先确认配置存在，不让用户输完 Key 才发现名字写错。只读一次配置。
			cfg, loadErr := config.Load(cmd.Context())
			if errors.Is(loadErr, config.ErrInvalidAI) {
				return fmt.Errorf("%w；请先手动修正或删除 ai 段，再使用 sec config ai", loadErr)
			}
			if loadErr != nil && !errors.Is(loadErr, config.ErrInvalid) {
				return loadErr
			}
			if !cfg.AI.Has(name) {
				// 配置文件读取有问题（比如根本不是 JSON）时，找不到配置可能是因为这个，一起说明。
				if loadErr != nil {
					return fmt.Errorf("%w：%s；读取配置时有问题：%v", config.ErrAIProfileNotFound, name, loadErr)
				}
				return fmt.Errorf("%w：%s", config.ErrAIProfileNotFound, name)
			}
			secret, err := config.DefaultAISecret()
			if err != nil {
				return err
			}
			before, beforeErr := os.Stat(secret.Path())
			key, err := readKey(cmd, name)
			if err != nil {
				return err
			}
			var ai config.AIConfig
			clearedEnv := ""
			if err := config.Update(cmd.Context(), func(c *config.Config) error {
				var err error
				if clearedEnv, err = c.AI.SetKey(secret, name, key); err != nil {
					return err
				}
				ai = c.AI
				return nil
			}); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "已保存 %s 的 Key：%s\n", name, ai.KeyStatus(secret, name, os.Getenv))
			if clearedEnv != "" {
				fmt.Fprintf(out, "已不再从环境变量 %s 读取 Key。\n", clearedEnv)
			}
			// 第一次生成或替换了损坏的密钥时提醒；替换是改名，和原来不是同一个文件。
			if after, err := os.Stat(secret.Path()); err == nil && (beforeErr != nil || !os.SameFile(before, after)) {
				fmt.Fprintf(out, "已生成加密密钥 %s，不要和 config.json 一起同步或备份。\n", secret.Path())
			}
			return nil
		},
	}
}

// readKey 读取 Key：终端里不回显地输入，否则（管道、重定向）从 stdin 读一行。提示写到 stderr，不混进输出。
func readKey(cmd *cobra.Command, name string) (string, error) {
	if file, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		return readHiddenKey(cmd, int(file.Fd()), name)
	}
	// stdin 不是终端：多半是管道，也可能是 Windows mintty 等不算终端的控制台，那样输入会显示出来，要说清楚。
	fmt.Fprintf(cmd.ErrOrStderr(), "从 stdin 读取 %s 的 API Key（stdin 不是终端，输入不会隐藏）\n", name)
	reader := bufio.NewReaderSize(cmd.InOrStdin(), maxKeyLength+1)
	line, err := reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return "", fmt.Errorf("输入的第一行超过 %d 字节，不像是一个 Key", maxKeyLength)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("读取 Key 失败：%w", err)
	}
	key := strings.TrimSpace(string(line))
	if key == "" {
		return "", errors.New("没有读到 Key：在终端里运行会提示输入，或从管道传入一行，如 pbpaste | sec config ai set-key " + name)
	}
	// 已经读进缓冲区的后续内容不是空白时说明传入了多行，比如粘贴错了文件；只看缓冲区，不为此再等输入。
	if rest, _ := reader.Peek(reader.Buffered()); strings.TrimSpace(string(rest)) != "" {
		return "", errors.New("输入有多行内容，Key 只能是一行，请检查是否传错了内容")
	}
	return key, nil
}

// readHiddenKey 在终端里不回显地读 Key。ReadPassword 关掉回显后靠 defer 恢复，按 Ctrl-C 时进程直接退出，
// 终端会一直不回显，所以读取期间自己处理 Ctrl-C：先恢复终端再退出。
func readHiddenKey(cmd *cobra.Command, fd int, name string) (string, error) {
	stderr := cmd.ErrOrStderr()
	if state, err := term.GetState(fd); err == nil {
		interrupt := make(chan os.Signal, 1)
		signal.Notify(interrupt, os.Interrupt)
		done := make(chan struct{})
		defer func() { signal.Stop(interrupt); close(done) }()
		go func() {
			select {
			case <-interrupt:
				_ = term.Restore(fd, state)
				fmt.Fprintln(stderr)
				os.Exit(130)
			case <-done:
			}
		}()
	}
	fmt.Fprintf(stderr, "输入 %s 的 API Key（不回显）：", name)
	key, err := term.ReadPassword(fd)
	fmt.Fprintln(stderr)
	if err != nil {
		return "", fmt.Errorf("读取 Key 失败：%w", err)
	}
	return string(key), nil
}

func newAIUnsetKeyCLI() *cobra.Command {
	return &cobra.Command{
		Use:   "unset-key <name>",
		Short: "Delete a profile's stored API key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := checkAI(cmd); err != nil {
				return err
			}
			var ai config.AIConfig
			had := false
			if err := config.Update(cmd.Context(), func(c *config.Config) error {
				var err error
				if had, err = c.AI.UnsetKey(name); err != nil {
					return err
				}
				ai = c.AI
				return nil
			}); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if !had {
				fmt.Fprintf(out, "%s 没有保存的 Key\n", name)
			} else {
				fmt.Fprintf(out, "已删除 %s 保存的 Key\n", name)
			}
			// 删除后可能还会从环境变量读到 Key，说清楚现在用的是什么。
			if secret, err := config.DefaultAISecret(); err == nil {
				fmt.Fprintf(out, "现在的 Key：%s\n", ai.KeyStatus(secret, name, os.Getenv))
			}
			return nil
		},
	}
}

func providerName(id string) string {
	if p, ok := config.LookupAIProvider(id); ok {
		return p.Name
	}
	return id
}
