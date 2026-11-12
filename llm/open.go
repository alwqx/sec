package llm

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/alwqx/sec/config"
)

var (
	// ErrNoProfile 表示还没有模型配置。
	ErrNoProfile = errors.New("还没有模型配置，用 sec config ai add 添加")
	// ErrAIDisabled 表示有模型配置但 AI 功能没有启用。默认关闭，用户主动启用后才调用模型。
	ErrAIDisabled = errors.New("AI 功能没有启用，用 sec config ai enable 启用（会先说明哪些数据会发给厂商）")
)

// Model 标识实际使用的模型。缓存键用协议、地址和模型名，不用配置名：改名或复制配置不会让缓存失效，换了模型才会重新调用。
type Model struct {
	Profile  string // 配置名，只用于显示
	Provider string // 厂商 id
	API      string
	BaseURL  string
	Name     string
	// ThinkingOff 和 ThinkingOffSupported 用于出错时给建议，比如推理用完输出上限时提示关闭思考；不进缓存键。
	ThinkingOff          bool
	ThinkingOffSupported bool
}

// ThinkingIgnoredHint 是配置关闭了思考、模型却仍在思考时的提示，连接测试和各功能出错时共用。
func ThinkingIgnoredHint(profile string) string {
	return fmt.Sprintf("配置关闭了思考，但模型仍在思考，这个模型可能不支持关闭；请换一个模型，或恢复默认：sec config ai edit %s --thinking \"\"，或在 Desktop 设置页「AI 模型」取消「关闭思考」", profileName(profile))
}

// profileName 是提示里的配置名，测试等场景没有名字时用占位符。
func profileName(name string) string {
	if name == "" {
		return "<配置名>"
	}
	return name
}

// ID 是缓存键里的模型部分。
func (m Model) ID() string { return m.API + " " + m.BaseURL + " " + m.Name }

// Open 按配置名创建客户端，name 为空时用 default。调用模型的功能（解读、摘要、对话）都从这里取客户端：
// 没有模型配置、AI 功能没有启用、Key 读不出来时返回错误，说明怎么处理。getenv 一般传 os.Getenv。
func Open(ai *config.AIConfig, name string, secret *config.AISecret, getenv func(string) string) (Client, Model, error) {
	if len(ai.Profiles) == 0 {
		return nil, Model{}, ErrNoProfile
	}
	if !ai.Enabled {
		return nil, Model{}, ErrAIDisabled
	}
	if name == "" {
		name = ai.Default
		slog.Debug("没有指定模型配置，使用默认配置", "profile", name)
	}
	profile, ok := ai.Profiles[name]
	if !ok {
		return nil, Model{}, fmt.Errorf("%w：%s，可选：%v", config.ErrAIProfileNotFound, name, ai.Names())
	}
	key, err := ai.ResolveKey(secret, name, getenv)
	if err != nil {
		return nil, Model{}, err
	}
	client, err := New(profile, key.Value)
	if err != nil {
		return nil, Model{}, err
	}
	return client, Model{
		Profile: name, Provider: profile.Provider, API: profile.EffectiveAPI(), BaseURL: profile.EffectiveBaseURL(), Name: profile.Model,
		ThinkingOff: profile.ThinkingDisabled(), ThinkingOffSupported: profile.ThinkingOffSupported(),
	}, nil
}
