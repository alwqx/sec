package config

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// 模型接口协议。同一种协议可以有多个厂商，比如 OpenAI、DeepSeek 和本地 Ollama 都用 openai。
const (
	AIAPIAnthropic = "anthropic" // Anthropic Messages API
	AIAPIOpenAI    = "openai"    // OpenAI 兼容接口
)

// AIAPIs 是支持的接口协议。
var AIAPIs = []string{AIAPIAnthropic, AIAPIOpenAI}

// AIProviderCustom 是自定义厂商：自建网关、代理等，必须自己填接口协议和地址。
const AIProviderCustom = "custom"

// AIProvider 是一个模型厂商的默认设置。配置里没有填 api、base_url 时用这里的值。
type AIProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"` // 显示名
	API  string `json:"api"`
	// BaseURL 是官方地址。有中国和国际两个区域的厂商用中国区域，Key 只在签发它的区域可用，国际区域请填 base_url。
	BaseURL string `json:"base_url"`
	// KeyEnv 是没有设置 Key 时读取的环境变量，本地模型和自定义厂商没有。
	KeyEnv string `json:"key_env,omitempty"`
}

// AIProviders 是支持的模型厂商，按显示顺序。地址和环境变量按各厂商官方文档，变化时在这里更新。
var AIProviders = []AIProvider{
	{AIAPIAnthropic, "Anthropic", AIAPIAnthropic, "https://api.anthropic.com", "ANTHROPIC_API_KEY"},
	{"openai", "OpenAI", AIAPIOpenAI, "https://api.openai.com/v1", "OPENAI_API_KEY"},
	{"deepseek", "DeepSeek", AIAPIOpenAI, "https://api.deepseek.com", "DEEPSEEK_API_KEY"},
	{"qwen", "通义千问（阿里云百炼）", AIAPIOpenAI, "https://dashscope.aliyuncs.com/compatible-mode/v1", "DASHSCOPE_API_KEY"},
	{"moonshot", "月之暗面 Kimi", AIAPIOpenAI, "https://api.moonshot.cn/v1", "MOONSHOT_API_KEY"},
	{"zhipu", "智谱 GLM", AIAPIOpenAI, "https://open.bigmodel.cn/api/paas/v4", "ZAI_API_KEY"},
	{"ollama", "Ollama（本地）", AIAPIOpenAI, "http://localhost:11434/v1", ""},
	{AIProviderCustom, "自定义", "", "", ""},
}

// LookupAIProvider 按 id 查找厂商。
func LookupAIProvider(id string) (AIProvider, bool) {
	i := slices.IndexFunc(AIProviders, func(p AIProvider) bool { return p.ID == id })
	if i < 0 {
		return AIProvider{}, false
	}
	return AIProviders[i], true
}

// AIProviderIDs 返回全部厂商 id，按显示顺序。
func AIProviderIDs() []string {
	ids := make([]string, 0, len(AIProviders))
	for _, p := range AIProviders {
		ids = append(ids, p.ID)
	}
	return ids
}

// AIConfig 是 config.json 的 ai 段：CLI 和 Desktop 共用的模型配置，设计见 docs/ai.md。
// 默认关闭，用户添加配置并启用后，调用模型的功能才可用。
type AIConfig struct {
	Enabled bool `json:"enabled"`
	// Default 是不指定配置时使用的配置名，有配置时必须是 Profiles 里的名字。
	Default string `json:"default"`
	// Profiles 以配置名为键，名字见 ValidateAIName。
	Profiles map[string]AIProfile `json:"profiles"`
}

// AIProfile 是一个模型配置：哪个厂商、用什么协议和地址、哪个模型、Key 从哪里来。
type AIProfile struct {
	// Provider 是厂商 id，见 AIProviders。
	Provider string `json:"provider"`
	// API 是接口协议，为空时用厂商的默认协议；自定义厂商必须填。部分厂商同时提供两种协议，比如 DeepSeek 的 Anthropic 兼容接口。
	API string `json:"api,omitempty"`
	// BaseURL 为空时用厂商的官方地址；自定义厂商必须填。
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model"`
	// APIKeyEnv 是读取 Key 的环境变量名，非空时优先于 APIKey。
	APIKeyEnv string `json:"api_key_env,omitempty"`
	// APIKey 是加密保存的 Key（enc:v1:...），明文 Key 不写入文件。本地模型可以不填。
	APIKey string `json:"api_key,omitempty"`
	// Thinking 为空时用厂商和模型的默认行为；AIThinkingOff 关闭思考，只支持 ThinkingOffSupported 的厂商和协议。
	Thinking string `json:"thinking,omitempty"`
}

// AIThinkingOff 关闭模型的思考（推理）。思考 token 也计入输出上限，长输入时可能用完上限、没有正文，也更慢。
const AIThinkingOff = "off"

// ErrThinkingOffUnsupported 表示这个厂商、协议或地址不能关闭思考，见 AIProfile.ThinkingOffSupported。
var ErrThinkingOffUnsupported = errors.New("暂不支持关闭思考")

// aiKeyPrefix 是加密 Key 的格式前缀，v1 是 AES-256-GCM，见 docs/ai.md。
const aiKeyPrefix = "enc:v1:"

var (
	aiName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	envVar = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// ErrInvalidKey 表示要保存的 Key 本身不对（为空、含空白字符），是用户输入的问题，区别于加密和读写 secret.key 的错误。
var ErrInvalidKey = errors.New("Key 无效")

// keyError 是 Key 无效的具体原因，文字不变，errors.Is 时算作 ErrInvalidKey。
type keyError string

func (e keyError) Error() string      { return string(e) }
func (keyError) Is(target error) bool { return target == ErrInvalidKey }

// ErrAIProfileNotFound 表示没有这个名字的模型配置。
var ErrAIProfileNotFound = errors.New("没有这个模型配置")

// ValidateAIName 检查配置名：小写字母、数字和 -，以字母或数字开头，最长 32 个字符。
func ValidateAIName(name string) error {
	if !aiName.MatchString(name) {
		// 不回显名字：误把 Key 当成名字时不打印出来。
		return errors.New("模型配置名无效，只能用小写字母、数字和 -，以字母或数字开头，最长 32 个字符")
	}
	return nil
}

// Validate 检查一个模型配置。
func (p AIProfile) Validate() error {
	provider, ok := LookupAIProvider(p.Provider)
	if !ok {
		// 校验错误会写进日志和终端，不回显填的值：Key 填错位置时不打印出来。
		return fmt.Errorf("厂商无效，只能是 %s；其他厂商或自建网关用 %s", strings.Join(AIProviderIDs(), "、"), AIProviderCustom)
	}
	if p.API != "" && !slices.Contains(AIAPIs, p.API) {
		return fmt.Errorf("接口协议无效，只能是 %s", strings.Join(AIAPIs, " 或 "))
	}
	if provider.ID == AIProviderCustom && (p.API == "" || p.BaseURL == "") {
		return fmt.Errorf("%s 厂商必须填接口协议（api）和接口地址（base_url）", AIProviderCustom)
	}
	// 官方地址只对应默认协议，换协议（如 DeepSeek 的 Anthropic 兼容接口）要填对应的地址。
	if p.API != "" && p.API != provider.API && p.BaseURL == "" {
		return fmt.Errorf("%s 的默认协议是 %s，改用 %s 时要填对应的接口地址（base_url）", provider.Name, provider.API, p.API)
	}
	if strings.TrimSpace(p.Model) == "" {
		return errors.New("模型不能为空")
	}
	switch {
	case p.Thinking != "" && p.Thinking != AIThinkingOff:
		return fmt.Errorf("thinking 只能为空（用厂商默认）或 %s（关闭思考）", AIThinkingOff)
	case p.Thinking == AIThinkingOff && !p.ThinkingOffSupported():
		return fmt.Errorf("%s %w，目前只支持 %s 官方地址的 OpenAI 兼容接口", provider.Name, ErrThinkingOffUnsupported, thinkingOffProviderNames())
	}
	// 校验错误会写进日志和终端：可能含密码的地址和填错位置的 Key 不回显。
	if p.BaseURL != "" {
		u, err := url.Parse(p.BaseURL)
		if err == nil && u.User != nil {
			return errors.New("接口地址不能带用户名和密码，Key 请用 set-key 或环境变量设置")
		}
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("接口地址无效，须为 http:// 或 https:// 开头的地址")
		}
		// 查询参数可以保留（Azure 等网关要 api-version），但不能用来放 Key：地址会明文写进文件和 sec config 的输出。
		if u.Fragment != "" || hasSecretParam(u.Query()) {
			return errors.New("接口地址不能带 # 片段，也不能在查询参数里放 Key、token 或密码，Key 请用 set-key 或环境变量设置")
		}
	}
	if p.APIKeyEnv != "" && !envVar.MatchString(p.APIKeyEnv) {
		return errors.New("api_key_env 应为环境变量名（字母、数字和下划线，如 DEEPSEEK_API_KEY），不是 Key 本身")
	}
	if p.APIKey != "" && !strings.HasPrefix(p.APIKey, aiKeyPrefix) {
		return fmt.Errorf("api_key 只能保存加密后的 Key（%s 开头），不能写明文", aiKeyPrefix)
	}
	return nil
}

// ThinkingOffSupported 判断能否关闭思考。各厂商关闭思考的参数不同，只在实现并核实过参数的厂商、协议和官方地址上允许，
// 避免发出厂商不认识的参数后被拒绝，或被忽略而以为已经关闭。改了地址的配置可能是转发同一模型的第三方服务，参数不一定相同。
// 只思考的模型仍可能忽略关闭参数，llm 从回复里发现并提示（Response.ThinkingIgnored）。不支持的有：Anthropic 协议（Claude
// 各模型规则不同，有的不能关闭）；OpenAI（只有 gpt-5.1 及以后支持，其他模型直接报错且不说明原因）；Kimi、Ollama（参数按模型而异）。
func (p AIProfile) ThinkingOffSupported() bool {
	return slices.Contains(thinkingOffProviders, p.Provider) && p.EffectiveAPI() == AIAPIOpenAI && p.officialBaseURL()
}

// thinkingOffProviderNames 是能关闭思考的厂商显示名，用于错误提示，和 thinkingOffProviders 保持一致。
func thinkingOffProviderNames() string {
	names := make([]string, 0, len(thinkingOffProviders))
	for _, id := range thinkingOffProviders {
		provider, _ := LookupAIProvider(id)
		names = append(names, provider.Name)
	}
	return strings.Join(names, "、")
}

// ThinkingOffProviderIDs 返回能关闭思考的厂商 id，供 Desktop 表单判断开关是否可用；最终以 Validate 为准。
func ThinkingOffProviderIDs() []string { return slices.Clone(thinkingOffProviders) }

// thinkingOffProviders 是能关闭思考的厂商（OpenAI 兼容接口），请求参数见 llm 的 openAIExtraParams。
var thinkingOffProviders = []string{"deepseek", "qwen", "zhipu"}

// officialBaseURL 判断实际地址是不是厂商的官方地址：没填，或只差结尾的 /。
func (p AIProfile) officialBaseURL() bool {
	provider, _ := LookupAIProvider(p.Provider)
	return p.BaseURL == "" || strings.TrimRight(p.BaseURL, "/") == provider.BaseURL
}

// EffectiveAPI 是实际使用的接口协议：填了用填的，没填用厂商的默认协议。
func (p AIProfile) EffectiveAPI() string {
	if p.API != "" {
		return p.API
	}
	provider, _ := LookupAIProvider(p.Provider)
	return provider.API
}

// EffectiveBaseURL 是实际使用的接口地址：填了用填的，没填用厂商的官方地址。
func (p AIProfile) EffectiveBaseURL() string {
	if p.BaseURL != "" {
		return p.BaseURL
	}
	provider, _ := LookupAIProvider(p.Provider)
	return provider.BaseURL
}

// DefaultKeyEnv 是没有设置 Key 时读取的厂商环境变量，按厂商而不是协议区分，DeepSeek 不会误用 OPENAI_API_KEY。
// 改了接口地址时不读：地址可能是别的服务，不能把厂商的 Key 发过去。
func (p AIProfile) DefaultKeyEnv() string {
	if !p.officialBaseURL() {
		return ""
	}
	provider, _ := LookupAIProvider(p.Provider)
	return provider.KeyEnv
}

// hasSecretParam 判断查询参数里有没有像是放 Key 的参数。
func hasSecretParam(query url.Values) bool {
	for name := range query {
		name = strings.ToLower(name)
		for _, word := range []string{"key", "token", "secret", "password", "passwd"} {
			if strings.Contains(name, word) {
				return true
			}
		}
	}
	return false
}

// IsLocal 判断实际地址是不是本机：本机模型的查询内容不出本机，用于隐私提示。按地址判断，指向其他主机的 Ollama 不算本机。
func (p AIProfile) IsLocal() bool {
	u, err := url.Parse(p.EffectiveBaseURL())
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Validate 检查 ai 段：每个配置有效，default 指向存在的配置，启用时至少有一个配置。
func (a AIConfig) Validate() error {
	// 按名字顺序校验，有几个无效配置时每次报同一个。
	for _, name := range a.Names() {
		if err := ValidateAIName(name); err != nil {
			return err
		}
		if err := a.Profiles[name].Validate(); err != nil {
			return fmt.Errorf("模型配置 %s：%w", name, err)
		}
	}
	// default 是有效的配置名时才回显。
	defaultName := "default"
	if ValidateAIName(a.Default) == nil {
		defaultName = fmt.Sprintf("default（%s）", a.Default)
	}
	switch {
	case len(a.Profiles) == 0 && a.Default != "":
		return fmt.Errorf("%s 指向的模型配置不存在，还没有模型配置", defaultName)
	case len(a.Profiles) > 0 && a.Default == "":
		return errors.New("有模型配置时 default 不能为空")
	case a.Default != "" && !a.Has(a.Default):
		return fmt.Errorf("%s 指向的模型配置不存在", defaultName)
	case a.Enabled && len(a.Profiles) == 0:
		return errors.New("还没有模型配置，不能启用")
	}
	return nil
}

// Has 判断是否有这个名字的配置。
func (a AIConfig) Has(name string) bool {
	_, ok := a.Profiles[name]
	return ok
}

// Names 返回全部配置名，按名字排序。
func (a AIConfig) Names() []string {
	return slices.Sorted(maps.Keys(a.Profiles))
}

// isZero 判断 ai 段是不是空的：没有启用、没有 default、没有配置。
func (a AIConfig) isZero() bool {
	return !a.Enabled && a.Default == "" && len(a.Profiles) == 0
}

// normalize 把空的 Profiles 换成 nil，没有配置时不写入 ai 段。
func (a *AIConfig) normalize() {
	if len(a.Profiles) == 0 {
		a.Profiles = nil
	}
}

// Add 添加一个配置，第一个配置自动成为 default。不能保存明文 Key，Key 用单独的方法设置。
func (a *AIConfig) Add(name string, p AIProfile) error {
	if err := ValidateAIName(name); err != nil {
		return err
	}
	if a.Has(name) {
		return fmt.Errorf("模型配置 %s 已存在", name)
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if a.Profiles == nil {
		a.Profiles = map[string]AIProfile{}
	}
	a.Profiles[name] = p
	if a.Default == "" {
		a.Default = name
	}
	return nil
}

// Edit 修改一个配置，修改后重新校验。厂商或实际地址变了时删除保存的 Key：
// 那是原来那个服务的 Key，不能发给新地址（厂商环境变量同样不会发，见 DefaultKeyEnv）。
func (a *AIConfig) Edit(name string, change func(*AIProfile)) error {
	p, ok := a.Profiles[name]
	if !ok {
		return fmt.Errorf("%w：%s", ErrAIProfileNotFound, name)
	}
	before := p
	change(&p)
	if p.Provider != before.Provider || strings.TrimRight(p.EffectiveBaseURL(), "/") != strings.TrimRight(before.EffectiveBaseURL(), "/") {
		p.APIKey = ""
	}
	if err := p.Validate(); err != nil {
		return err
	}
	a.Profiles[name] = p
	return nil
}

// Remove 删除一个配置。不能删除 default 指向的配置，要先把 default 改成别的配置；
// 只剩这一个配置时可以删除，删除后没有 default，并关闭 AI 功能。
func (a *AIConfig) Remove(name string) error {
	if !a.Has(name) {
		return fmt.Errorf("%w：%s", ErrAIProfileNotFound, name)
	}
	if name == a.Default && len(a.Profiles) > 1 {
		return fmt.Errorf("%s 是默认配置（default），不能删除，请先把别的配置设为默认", name)
	}
	delete(a.Profiles, name)
	if len(a.Profiles) == 0 {
		a.Profiles, a.Default, a.Enabled = nil, "", false
	}
	return nil
}

// Use 把 default 改成这个配置。
func (a *AIConfig) Use(name string) error {
	if !a.Has(name) {
		return fmt.Errorf("%w：%s", ErrAIProfileNotFound, name)
	}
	a.Default = name
	return nil
}

// SetEnabled 启用或关闭 AI 功能，启用前至少要有一个配置。
func (a *AIConfig) SetEnabled(enabled bool) error {
	if enabled && len(a.Profiles) == 0 {
		return errors.New("还没有模型配置，请先添加再启用")
	}
	a.Enabled = enabled
	return nil
}

// Redacted 返回去掉 Key 的副本，用于显示和接口返回：保存了 Key 的配置只标明「已加密保存」。
func (a AIConfig) Redacted() AIConfig {
	out := a
	if a.Profiles != nil {
		out.Profiles = make(map[string]AIProfile, len(a.Profiles))
		for name, p := range a.Profiles {
			if p.APIKey != "" {
				p.APIKey = RedactedKey
			}
			out.Profiles[name] = p
		}
	}
	return out
}

// RedactedKey 是显示时代替加密 Key 的文字。
const RedactedKey = "（已加密保存）"

// 读取 Key 的来源，顺序见 ResolveKey。
const (
	AIKeyFromEnv         = "env"          // api_key_env 指定的环境变量
	AIKeyStored          = "stored"       // config.json 里加密保存的 Key
	AIKeyFromProviderEnv = "provider_env" // 厂商默认的环境变量
	AIKeyNone            = "none"         // 没有 Key，本地模型可以不填
)

// AIKey 是读到的 Key 和它的来源。
type AIKey struct {
	Value  string
	Source string // AIKeyFromEnv 等
	Env    string // 来源是环境变量时的变量名
}

// SetKey 加密保存一个配置的 Key，没有 secret.key 时先生成。Key 去掉首尾空白后不能为空，也不能含空白字符。
// 保存的 Key 排在 api_key_env 之后，所以同时清空 api_key_env，否则保存的 Key 用不上；返回被清空的环境变量名，供命令提示。
func (a *AIConfig) SetKey(secret *AISecret, name, key string) (string, error) {
	p, ok := a.Profiles[name]
	if !ok {
		return "", fmt.Errorf("%w：%s", ErrAIProfileNotFound, name)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", keyError("Key 不能为空")
	}
	if strings.ContainsAny(key, " \t\r\n") {
		return "", keyError("Key 不能包含空白字符，请检查是否多粘贴了内容")
	}
	encrypted, err := secret.Encrypt(name, key)
	if err != nil {
		return "", err
	}
	clearedEnv := p.APIKeyEnv
	p.APIKey, p.APIKeyEnv = encrypted, ""
	a.Profiles[name] = p
	return clearedEnv, nil
}

// UnsetKey 删除一个配置保存的 Key，返回原来有没有保存。
func (a *AIConfig) UnsetKey(name string) (bool, error) {
	p, ok := a.Profiles[name]
	if !ok {
		return false, fmt.Errorf("%w：%s", ErrAIProfileNotFound, name)
	}
	had := p.APIKey != ""
	p.APIKey = ""
	a.Profiles[name] = p
	return had, nil
}

// ResolveKey 读取一个配置的 Key，顺序：
//  1. api_key_env 非空时读这个环境变量，没有设置时报错，不往下找；
//  2. 否则解密 api_key；
//  3. 都没有时读厂商的默认环境变量（见 DefaultKeyEnv）。
//
// 都没有时返回 AIKeyNone，本地模型可以不用 Key，由调用方决定是否报错。getenv 一般传 os.Getenv。
func (a *AIConfig) ResolveKey(secret *AISecret, name string, getenv func(string) string) (AIKey, error) {
	p, ok := a.Profiles[name]
	if !ok {
		return AIKey{}, fmt.Errorf("%w：%s", ErrAIProfileNotFound, name)
	}
	if p.APIKeyEnv != "" {
		if value := strings.TrimSpace(getenv(p.APIKeyEnv)); value != "" {
			return AIKey{Value: value, Source: AIKeyFromEnv, Env: p.APIKeyEnv}, nil
		}
		return AIKey{}, fmt.Errorf("模型配置 %s 从环境变量 %s 读取 Key，但这个环境变量没有设置", name, p.APIKeyEnv)
	}
	if p.APIKey != "" {
		value, err := secret.Decrypt(name, p.APIKey)
		if err != nil {
			return AIKey{}, fmt.Errorf("模型配置 %s：%w", name, err)
		}
		return AIKey{Value: value, Source: AIKeyStored}, nil
	}
	if env := p.DefaultKeyEnv(); env != "" {
		if value := strings.TrimSpace(getenv(env)); value != "" {
			return AIKey{Value: value, Source: AIKeyFromProviderEnv, Env: env}, nil
		}
	}
	return AIKey{Source: AIKeyNone}, nil
}

// KeyStatus 是一个配置的 Key 的显示文字：来源和末 4 位，不显示 Key 本身。
func (a *AIConfig) KeyStatus(secret *AISecret, name string, getenv func(string) string) string {
	key, err := a.ResolveKey(secret, name, getenv)
	return a.DescribeKey(name, key, err)
}

// DescribeKey 把 ResolveKey 的结果换成 KeyStatus 的显示文字。已经调用过 ResolveKey 时用它，不用再读一次 secret.key。
func (a *AIConfig) DescribeKey(name string, key AIKey, err error) string {
	switch {
	// 只给简短状态，完整原因（含路径）在 ResolveKey 的错误里。
	case errors.Is(err, ErrSecretMissing), errors.Is(err, ErrKeyUndecryptable):
		return "加密保存，无法解密，请重新设置"
	case errors.Is(err, ErrSecretPermissions):
		return "加密保存，secret.key 权限过宽，请改为 600"
	case errors.Is(err, ErrSecretCorrupt):
		return "加密保存，secret.key 已损坏，请重新设置"
	case err != nil && a.Profiles[name].APIKeyEnv == "":
		return "加密保存，读取 secret.key 失败"
	case err != nil:
		return err.Error()
	}
	switch key.Source {
	case AIKeyFromEnv:
		return "环境变量 " + key.Env + lastFour(key.Value)
	case AIKeyStored:
		return "加密保存" + lastFour(key.Value)
	case AIKeyFromProviderEnv:
		return "厂商环境变量 " + key.Env + lastFour(key.Value)
	}
	if a.Profiles[name].IsLocal() {
		return "未设置（本机模型可以不填）"
	}
	return "未设置"
}

// AIProfileView 是给用户看的一个配置：协议和地址是实际使用的值，Key 只给来源和末 4 位。
// sec config ai list --json 和 Desktop 设置页共用。
type AIProfileView struct {
	Name         string `json:"name"`
	Default      bool   `json:"default"`
	Provider     string `json:"provider"`
	ProviderName string `json:"provider_name"`
	API          string `json:"api"`
	BaseURL      string `json:"base_url"`
	Model        string `json:"model"`
	APIKeyEnv    string `json:"api_key_env,omitempty"`
	Key          string `json:"key"`
	KeyStored    bool   `json:"key_stored"` // 有没有加密保存的 Key，Key 也可能来自环境变量
	Local        bool   `json:"local"`
	Thinking     string `json:"thinking,omitempty"`
}

// Views 按名字顺序返回全部配置的显示信息，没有配置时是空切片。getenv 一般传 os.Getenv。
func (a *AIConfig) Views(secret *AISecret, getenv func(string) string) []AIProfileView {
	views := []AIProfileView{}
	for _, name := range a.Names() {
		p := a.Profiles[name]
		provider, _ := LookupAIProvider(p.Provider)
		views = append(views, AIProfileView{
			Name: name, Default: name == a.Default, Provider: p.Provider, ProviderName: provider.Name,
			API: p.EffectiveAPI(), BaseURL: p.EffectiveBaseURL(), Model: p.Model, APIKeyEnv: p.APIKeyEnv,
			Key: a.KeyStatus(secret, name, getenv), KeyStored: p.APIKey != "", Local: p.IsLocal(), Thinking: p.Thinking,
		})
	}
	return views
}

// lastFour 是 Key 的末 4 位；太短的 Key 不显示，避免露出大半；含非 ASCII 或不可打印字符时也不显示，避免切出乱码。
func lastFour(key string) string {
	if len(key) < 12 || strings.ContainsFunc(key, func(r rune) bool { return r < '!' || r > '~' }) {
		return ""
	}
	return "（末 4 位 " + key[len(key)-4:] + "）"
}
