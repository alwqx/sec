package config

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var deepseek = AIProfile{Provider: "deepseek", Model: "deepseek-chat"}

func TestAIProfileValidate(t *testing.T) {
	for _, p := range []AIProfile{
		{Provider: "anthropic", Model: "claude-sonnet-5-5"},
		deepseek,
		{Provider: "openai", BaseURL: "http://127.0.0.1:11434/v1", Model: "qwen3", APIKeyEnv: "OLLAMA_KEY"},
		{Provider: "anthropic", Model: "m", APIKey: "enc:v1:abc"},
		{Provider: "deepseek", API: AIAPIAnthropic, BaseURL: "https://api.deepseek.com/anthropic", Model: "deepseek-chat"},
		{Provider: AIProviderCustom, API: AIAPIOpenAI, BaseURL: "https://llm.corp.example/v1", Model: "x"},
	} {
		require.NoError(t, p.Validate(), p)
	}
	// 填错位置的 Key 和地址里的密码不出现在错误里，地址解析失败时也一样。
	require.NotContains(t, AIProfile{Provider: "openai", Model: "m", APIKeyEnv: "sk-secret"}.Validate().Error(), "sk-secret")
	for _, u := range []string{"https://u:p4ss@example.com", "https://u:p4ss@host:bad", "sk-p4ss", "https://x.example/v1?api_key=p4ss", "https://x.example/v1?access_token=p4ss"} {
		require.NotContains(t, AIProfile{Provider: "openai", Model: "m", BaseURL: u}.Validate().Error(), "p4ss", u)
	}
	require.NotContains(t, AIProfile{Provider: "sk-p4ss", Model: "m"}.Validate().Error(), "p4ss", "不回显厂商")
	require.NotContains(t, AIProfile{Provider: "openai", API: "sk-p4ss", Model: "m"}.Validate().Error(), "p4ss", "不回显协议")
	require.NoError(t, AIProfile{Provider: AIProviderCustom, API: AIAPIOpenAI, Model: "m", BaseURL: "https://x.openai.azure.com/openai/v1?api-version=preview"}.Validate(), "普通查询参数可以保留")
	for want, p := range map[string]AIProfile{
		"厂商无效":    {Provider: "gemini", Model: "m"},
		"接口协议无效":  {Provider: "openai", API: "grpc", Model: "m"},
		"必须填接口协议": {Provider: AIProviderCustom, BaseURL: "https://x.example", Model: "m"},
		"和接口地址":   {Provider: AIProviderCustom, API: AIAPIOpenAI, Model: "m"},
		"改用 anthropic 时要填对应的接口地址": {Provider: "deepseek", API: AIAPIAnthropic, Model: "m"},
		"不能带 # 片段":                {Provider: "openai", Model: "m", BaseURL: "https://x.example/v1#k"},
		"查询参数里放 Key":              {Provider: "openai", Model: "m", BaseURL: "https://x.example/v1?Api-Key=1"},
		"模型不能为空":                  {Provider: "openai", Model: " "},
		"接口地址无效":                  {Provider: "openai", Model: "m", BaseURL: "api.deepseek.com"},
		"不能带用户名和密码":               {Provider: "openai", Model: "m", BaseURL: "https://user:pass@example.com"},
		"不是 Key 本身":               {Provider: "openai", Model: "m", APIKeyEnv: "MY-KEY"},
		"不能写明文":                   {Provider: "openai", Model: "m", APIKey: "sk-123"},
	} {
		require.ErrorContains(t, p.Validate(), want)
	}
}

func TestValidateAIName(t *testing.T) {
	for _, name := range []string{"claude", "deepseek-r1", "0", strings.Repeat("a", 32)} {
		require.NoError(t, ValidateAIName(name), name)
	}
	for _, name := range []string{"", "Claude", "-a", "a_b", "a b", "通义", strings.Repeat("a", 33)} {
		require.Error(t, ValidateAIName(name), name)
	}
	require.NotContains(t, ValidateAIName("sk-Proj_123").Error(), "sk-Proj", "不回显名字")
	ai := AIConfig{Default: "sk-Proj_123", Profiles: map[string]AIProfile{"deepseek": deepseek}}
	require.NotContains(t, ai.Validate().Error(), "sk-Proj", "无效的 default 也不回显")
}

func TestAIConfigOperations(t *testing.T) {
	var ai AIConfig
	require.NoError(t, ai.Validate(), "空的 ai 段有效")
	require.ErrorContains(t, ai.SetEnabled(true), "请先添加")

	require.NoError(t, ai.Add("deepseek", deepseek))
	require.Equal(t, "deepseek", ai.Default, "第一个配置自动成为 default")
	require.NoError(t, ai.Add("claude", AIProfile{Provider: "anthropic", Model: "claude-sonnet-5-5"}))
	require.Equal(t, "deepseek", ai.Default)
	require.Equal(t, []string{"claude", "deepseek"}, ai.Names())
	require.ErrorContains(t, ai.Add("claude", deepseek), "已存在")
	require.ErrorContains(t, ai.Add("Bad", deepseek), "配置名无效")
	require.ErrorContains(t, ai.Add("x", AIProfile{Provider: "openai"}), "模型不能为空")
	require.NoError(t, ai.Validate())

	require.NoError(t, ai.Edit("deepseek", func(p *AIProfile) { p.Model = "deepseek-reasoner" }))
	require.Equal(t, "deepseek-reasoner", ai.Profiles["deepseek"].Model)
	require.ErrorContains(t, ai.Edit("deepseek", func(p *AIProfile) { p.Provider = "x" }), "厂商无效")
	require.Equal(t, "deepseek", ai.Profiles["deepseek"].Provider, "校验失败时不修改")
	require.ErrorIs(t, ai.Edit("nosuch", func(*AIProfile) {}), ErrAIProfileNotFound)

	require.NoError(t, ai.SetEnabled(true))
	require.ErrorContains(t, ai.Remove("deepseek"), "是 default，不能删除")
	require.ErrorIs(t, ai.Use("nosuch"), ErrAIProfileNotFound)
	require.NoError(t, ai.Use("claude"))
	require.NoError(t, ai.Remove("deepseek"))
	require.Equal(t, []string{"claude"}, ai.Names())
	require.ErrorIs(t, ai.Remove("deepseek"), ErrAIProfileNotFound)

	// 删除最后一个配置：没有 default，并关闭。
	require.NoError(t, ai.Remove("claude"))
	require.Equal(t, AIConfig{}, ai)
	require.NoError(t, ai.Validate())
}

func TestAIConfigValidate(t *testing.T) {
	for want, ai := range map[string]AIConfig{
		"还没有模型配置":                    {Default: "claude"},
		"default 不能为空":               {Profiles: map[string]AIProfile{"deepseek": deepseek}},
		"default（claude） 指向的模型配置不存在": {Default: "claude", Profiles: map[string]AIProfile{"deepseek": deepseek}},
		"不能启用":                       {Enabled: true},
		"配置名无效":                      {Default: "Deep", Profiles: map[string]AIProfile{"Deep": deepseek}},
		"模型配置 bad":                   {Default: "bad", Profiles: map[string]AIProfile{"bad": {Provider: "x", Model: "m"}}},
	} {
		require.ErrorContains(t, ai.Validate(), want)
	}
	// 几个配置都无效时按名字顺序报第一个。
	providers := map[string]AIProfile{}
	for _, name := range []string{"e", "c", "a", "d", "b"} {
		providers[name] = AIProfile{Provider: "x", Model: "m"}
	}
	for range 20 {
		require.ErrorContains(t, AIConfig{Default: "a", Profiles: providers}.Validate(), "模型配置 a：")
	}
}

func TestAIRedacted(t *testing.T) {
	ai := AIConfig{Enabled: true, Default: "claude", Profiles: map[string]AIProfile{
		"claude": {Provider: "anthropic", Model: "m", APIKey: "enc:v1:secret"},
		"local":  {Provider: "openai", Model: "qwen3"},
	}}
	out := (Config{SECEmail: DefaultSECEmail, AI: ai}).Redacted()
	require.Equal(t, RedactedKey, out.AI.Profiles["claude"].APIKey)
	require.Empty(t, out.AI.Profiles["local"].APIKey, "没有 Key 时不显示")
	require.Equal(t, "enc:v1:secret", ai.Profiles["claude"].APIKey, "不修改原配置")
}

func TestAIFile(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.json")

	// 没有 ai 段的旧文件照常读取，保存时也不写空的 ai 段。
	require.NoError(t, os.WriteFile(path, []byte(`{"sec_edgar_email":"me@example.org"}`), 0600))
	cfg, err := LoadFile(ctx, path)
	require.NoError(t, err)
	require.Equal(t, AIConfig{}, cfg.AI)
	require.NoError(t, UpdateFile(ctx, path, func(c *Config) error { c.SECEmail = "you@example.org"; return nil }))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(data), `"ai"`)

	require.NoError(t, UpdateFile(ctx, path, func(c *Config) error { return c.AI.Add("deepseek", deepseek) }))
	cfg, err = LoadFile(ctx, path)
	require.NoError(t, err)
	require.Equal(t, "deepseek", cfg.AI.Default)
	require.Equal(t, deepseek, cfg.AI.Profiles["deepseek"])
	require.Equal(t, "you@example.org", cfg.SECEmail)
	var raw map[string]any
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &raw))
	require.NotContains(t, raw["ai"].(map[string]any)["profiles"].(map[string]any)["deepseek"], "api_key", "空字段不写入")

	// 修改结果无效时不保存。
	require.ErrorContains(t, UpdateFile(ctx, path, func(c *Config) error { c.AI.Default = "nosuch"; return nil }), "ai 段")
	cfg, err = LoadFile(ctx, path)
	require.NoError(t, err)
	require.Equal(t, "deepseek", cfg.AI.Default)

	// 邮箱无效时只换掉邮箱，模型配置保留。
	require.NoError(t, os.WriteFile(path, []byte(`{"sec_edgar_email":"me","ai":{"enabled":true,"default":"deepseek","profiles":{"deepseek":{"provider":"openai","model":"deepseek-chat"}}}}`), 0600))
	cfg, err = LoadFile(ctx, path)
	require.ErrorIs(t, err, ErrInvalid)
	require.False(t, errors.Is(err, ErrInvalidAI))
	require.Equal(t, DefaultSECEmail, cfg.SECEmail)
	require.True(t, cfg.AI.Enabled)
	require.NoError(t, UpdateFile(ctx, path, func(c *Config) error { c.SECEmail = "fixed@example.org"; return nil }))
	cfg, err = LoadFile(ctx, path)
	require.NoError(t, err)
	require.True(t, cfg.AI.Has("deepseek"), "修正邮箱不会丢掉模型配置")

	// ai 段无效：读取时忽略；修改其他配置照常保存，ai 段原样写回；修改 ai 段时不保存。
	bad := `{"sec_edgar_email":"me@example.org","ai":{"default":"nosuch","profiles":{"deepseek":{"provider":"openai","model":"m","api_key":"enc:v1:keep"}}}}`
	require.NoError(t, os.WriteFile(path, []byte(bad), 0600))
	cfg, err = LoadFile(ctx, path)
	require.ErrorIs(t, err, ErrInvalidAI)
	require.ErrorIs(t, err, ErrInvalid, "ErrInvalidAI 也是 ErrInvalid，命令行按警告处理")
	require.ErrorContains(t, err, "已忽略 ai 段")
	require.Equal(t, AIConfig{}, cfg.AI)
	require.Equal(t, "me@example.org", cfg.SECEmail)
	rawAI := aiSection(t, path)
	require.NoError(t, UpdateFile(ctx, path, func(c *Config) error { c.SECEmail = "you@example.org"; return nil }))
	cfg, err = LoadFile(ctx, path)
	require.ErrorIs(t, err, ErrInvalidAI, "ai 段仍然无效")
	require.Equal(t, "you@example.org", cfg.SECEmail, "其他配置照常保存")
	require.JSONEq(t, rawAI, aiSection(t, path), "ai 段原样写回")

	before, err := os.ReadFile(path)
	require.NoError(t, err)
	err = UpdateFile(ctx, path, func(c *Config) error { return c.AI.Add("claude", AIProfile{Provider: "anthropic", Model: "m"}) })
	require.ErrorIs(t, err, ErrInvalidAI, "修改 ai 段时不保存")
	require.ErrorContains(t, err, "手动修正")
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(before), string(data), "文件不变")
	require.ErrorContains(t, UpdateFile(ctx, path, func(c *Config) error { c.SECEmail = "bad"; return nil }), "SEC 联系邮箱无效", "其他配置照常校验")

	// ai 段类型不对：其余配置照常读取，修改其他配置时 ai 段同样原样写回。
	typed := `{"sec_edgar_email":"me@example.org","ai":{"enabled":"yes","profiles":{"deepseek":{"provider":"openai","model":"m","api_key":"enc:v1:keep"}}}}`
	require.NoError(t, os.WriteFile(path, []byte(typed), 0600))
	cfg, err = LoadFile(ctx, path)
	require.ErrorIs(t, err, ErrInvalidAI)
	require.Equal(t, "me@example.org", cfg.SECEmail)
	require.Equal(t, AIConfig{}, cfg.AI)
	rawAI = aiSection(t, path)
	require.NoError(t, UpdateFile(ctx, path, func(c *Config) error { c.SECEmail = "you@example.org"; return nil }))
	require.JSONEq(t, rawAI, aiSection(t, path))
	require.Contains(t, aiSection(t, path), "enc:v1:keep")

	// 其他字段类型不对时只换掉它们，ai 段保留，修改时也不丢。
	valid := `{"sec_edgar_email":123,"ai":{"default":"ds","profiles":{"ds":{"provider":"deepseek","model":"m","api_key":"enc:v1:keep"}}}}`
	require.NoError(t, os.WriteFile(path, []byte(valid), 0600))
	cfg, err = LoadFile(ctx, path)
	require.ErrorIs(t, err, ErrInvalid)
	require.False(t, errors.Is(err, ErrInvalidAI))
	require.Equal(t, DefaultSECEmail, cfg.SECEmail)
	require.Equal(t, "enc:v1:keep", cfg.AI.Profiles["ds"].APIKey)
	require.NoError(t, UpdateFile(ctx, path, func(c *Config) error { c.SECEmail = "you@example.org"; return nil }))
	cfg, err = LoadFile(ctx, path)
	require.NoError(t, err)
	require.Equal(t, "enc:v1:keep", cfg.AI.Profiles["ds"].APIKey, "修正其他字段不会丢掉 Key")

	// 字段名大小写不同也算 ai 段（encoding/json 不分大小写）。
	upper := `{"sec_edgar_email":"me@example.org","AI":{"enabled":"yes","profiles":{}}}`
	require.NoError(t, os.WriteFile(path, []byte(upper), 0600))
	_, err = LoadFile(ctx, path)
	require.ErrorIs(t, err, ErrInvalidAI)
	require.NoError(t, UpdateFile(ctx, path, func(c *Config) error { return nil }))
	require.JSONEq(t, `{"enabled":"yes","profiles":{}}`, aiSection(t, path), "大写的 AI 段也原样写回")
	_, err = LoadFile(ctx, path)
	require.ErrorIs(t, err, ErrInvalidAI)

	// 不是 JSON 对象：从默认值开始，修改时覆盖。
	for _, broken := range []string{`{"ai":`, `[]`, `"x"`} {
		require.NoError(t, os.WriteFile(path, []byte(broken), 0600))
		_, err = LoadFile(ctx, path)
		require.ErrorIs(t, err, ErrInvalid, broken)
		require.False(t, errors.Is(err, ErrInvalidAI), broken)
		require.NoError(t, UpdateFile(ctx, path, func(c *Config) error { c.SECEmail = "you@example.org"; return nil }), broken)
	}

	// 空的 profiles 不写回空的 ai 段。
	require.NoError(t, os.WriteFile(path, []byte(`{"sec_edgar_email":"me@example.org","ai":{"profiles":{}}}`), 0600))
	require.NoError(t, UpdateFile(ctx, path, func(c *Config) error { c.SECEmail = "you@example.org"; return nil }))
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(data), `"ai"`)
}

func TestAIProviders(t *testing.T) {
	ids := map[string]bool{}
	for _, p := range AIProviders {
		require.False(t, ids[p.ID], "id 不重复：%s", p.ID)
		ids[p.ID] = true
		require.NotEmpty(t, p.Name)
		if p.ID == AIProviderCustom {
			continue
		}
		require.Contains(t, AIAPIs, p.API, p.ID)
		require.NoError(t, AIProfile{Provider: p.ID, Model: "m"}.Validate(), "官方地址有效：%s", p.ID)
	}
	_, ok := LookupAIProvider("gemini")
	require.False(t, ok)
}

func TestAIProfileDefaults(t *testing.T) {
	ds := AIProfile{Provider: "deepseek", Model: "deepseek-chat"}
	require.Equal(t, AIAPIOpenAI, ds.EffectiveAPI())
	require.Equal(t, "https://api.deepseek.com", ds.EffectiveBaseURL())
	require.Equal(t, "DEEPSEEK_API_KEY", ds.DefaultKeyEnv(), "按厂商取环境变量，不是协议的 OPENAI_API_KEY")
	require.False(t, ds.IsLocal())

	// 改用 Anthropic 兼容接口：协议和地址用填的，地址变了不读厂商的环境变量。
	ds.API, ds.BaseURL = AIAPIAnthropic, "https://api.deepseek.com/anthropic"
	require.Equal(t, AIAPIAnthropic, ds.EffectiveAPI())
	require.Equal(t, "https://api.deepseek.com/anthropic", ds.EffectiveBaseURL())
	require.Empty(t, ds.DefaultKeyEnv(), "地址可能是别的服务，不把厂商的 Key 发过去")
	ds.BaseURL = "https://api.deepseek.com"
	require.Equal(t, "DEEPSEEK_API_KEY", ds.DefaultKeyEnv(), "填的就是官方地址时照常读")
	ds.BaseURL = "https://api.deepseek.com/"
	require.Equal(t, "DEEPSEEK_API_KEY", ds.DefaultKeyEnv(), "结尾多一个斜杠也是官方地址")

	local := AIProfile{Provider: "ollama", Model: "qwen3"}
	require.True(t, local.IsLocal())
	require.Empty(t, local.DefaultKeyEnv())
	local.BaseURL = "http://gpu.lan:11434/v1"
	require.False(t, local.IsLocal(), "指向其他主机的 Ollama 不是本机")
	for _, u := range []string{"http://127.0.0.1:8080/v1", "http://[::1]:8080/v1", "http://localhost/v1"} {
		require.True(t, AIProfile{Provider: AIProviderCustom, API: AIAPIOpenAI, BaseURL: u}.IsLocal(), u)
	}
	require.Empty(t, AIProfile{Provider: AIProviderCustom, API: AIAPIOpenAI, BaseURL: "https://x.example"}.DefaultKeyEnv())
}

// aiSection 返回文件里 ai 段的原始 JSON，字段名不分大小写。
func aiSection(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &fields))
	raw := takeAI(fields)
	require.NotNil(t, raw, "文件里有 ai 段")
	return string(raw)
}

func TestAIEditClearsKeyOnEndpointChange(t *testing.T) {
	ai := AIConfig{Default: "ds", Profiles: map[string]AIProfile{"ds": {Provider: "deepseek", Model: "m", APIKey: "enc:v1:keep"}}}
	keep := func() { p := ai.Profiles["ds"]; p.APIKey = "enc:v1:keep"; ai.Profiles["ds"] = p }

	require.NoError(t, ai.Edit("ds", func(p *AIProfile) { p.Model = "deepseek-reasoner" }))
	require.Equal(t, "enc:v1:keep", ai.Profiles["ds"].APIKey, "只改模型时保留")
	require.NoError(t, ai.Edit("ds", func(p *AIProfile) { p.BaseURL = "https://api.deepseek.com/" }))
	require.Equal(t, "enc:v1:keep", ai.Profiles["ds"].APIKey, "填的就是官方地址（只差结尾斜杠）时保留")

	require.NoError(t, ai.Edit("ds", func(p *AIProfile) { p.BaseURL = "https://llm.other.example/v1" }))
	require.Empty(t, ai.Profiles["ds"].APIKey, "地址变了，原来的 Key 不能发过去")
	keep()
	require.NoError(t, ai.Edit("ds", func(p *AIProfile) { p.BaseURL = "" }))
	require.Empty(t, ai.Profiles["ds"].APIKey, "改回官方地址也是换了服务")
	keep()
	require.NoError(t, ai.Edit("ds", func(p *AIProfile) { p.Provider = "openai" }))
	require.Empty(t, ai.Profiles["ds"].APIKey, "换了厂商")
}
