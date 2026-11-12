package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/llm"
)

// aiSettings 是设置页「AI 模型」读取的模型配置，和 sec config ai list --json 一致，Key 只给来源和末 4 位。
type aiSettings struct {
	Enabled  bool                   `json:"enabled"`
	Default  string                 `json:"default"`
	Profiles []config.AIProfileView `json:"profiles"`
	// Providers 是内置厂商和它们的默认协议、地址，供新增和编辑表单使用。
	Providers []config.AIProvider `json:"providers"`
	// Invalid 是 ai 段无效的原因：这时按没有配置显示，提示用户手动修正。
	Invalid string `json:"invalid,omitempty"`
	// Notice 是修改后的提示，比如编辑后保存的 Key 被删除，只在修改接口的返回里有。
	Notice string `json:"notice,omitempty"`
}

// aiProfileInput 是新增和编辑表单提交的配置。不含 Key：Key 用单独的接口保存，不随配置提交。
type aiProfileInput struct {
	Name      string `json:"name,omitempty"` // 只在新增时用，编辑时名字在路径里
	Provider  string `json:"provider"`
	API       string `json:"api"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	APIKeyEnv string `json:"api_key_env"`
}

// aiSecret 返回加密 Key 用的 secret.key；测试时放在工作台文件旁边，和共享配置一样。
func (s *desktopServer) aiSecret() (*config.AISecret, error) {
	if s.workspace.path != "" {
		return config.NewAISecret(filepath.Join(filepath.Dir(s.workspace.path), "secret.key")), nil
	}
	return config.DefaultAISecret()
}

// loadAISettings 读取模型配置。ai 段或整个文件无效时不报错，原因放在 Invalid 里。
func (s *desktopServer) loadAISettings(ctx context.Context) (aiSettings, error) {
	path, err := s.configPath()
	if err != nil {
		return aiSettings{}, err
	}
	cfg, err := config.LoadFile(ctx, path)
	var invalid string
	switch {
	case errors.Is(err, config.ErrInvalidAI):
		invalid = aiProblem(err)
	// 整个文件无效时 ai 段也换成了默认值，不能显示「还没有模型配置」：照着 sec config ai add 会覆盖文件、丢掉已存的 Key。
	case errors.Is(err, config.ErrInvalidJSON):
		invalid = err.Error() + "；读不到模型配置"
	case errors.Is(err, config.ErrInvalid):
		slog.WarnContext(ctx, "共享配置无效，使用默认值", "error", err)
	case err != nil:
		return aiSettings{}, err
	}
	secret, err := s.aiSecret()
	if err != nil {
		return aiSettings{}, err
	}
	ai := cfg.AI
	return aiSettings{Enabled: ai.Enabled, Default: ai.Default, Profiles: ai.Views(secret, os.Getenv), Providers: config.AIProviders, Invalid: invalid}, nil
}

func (s *desktopServer) handleAISettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.loadAISettings(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "读取模型配置失败", "error", err)
		writeError(w, http.StatusInternalServerError, "读取模型配置失败，请检查 ~/.sec/config.json")
		return
	}
	writeJSON(w, settings)
}

func (s *desktopServer) handleAIAddProfile(w http.ResponseWriter, r *http.Request) {
	var in aiProfileInput
	if !s.readAIRequest(w, r, &in) {
		return
	}
	p := in.profile()
	s.changeAI(w, r, func(ai *config.AIConfig) (string, error) {
		if err := ai.Add(in.Name, p); err != nil {
			return "", err
		}
		if env := p.DefaultKeyEnv(); env != "" && p.APIKeyEnv == "" {
			return "Key 可以用环境变量 " + env + " 提供，或点「设置 Key」加密保存。", nil
		}
		return "", nil
	})
}

func (s *desktopServer) handleAIEditProfile(w http.ResponseWriter, r *http.Request) {
	var in aiProfileInput
	if !s.readAIRequest(w, r, &in) {
		return
	}
	name := r.PathValue("name")
	s.changeAI(w, r, func(ai *config.AIConfig) (string, error) {
		hadKey := ai.Profiles[name].APIKey != ""
		thinkingReset := false
		// 表单提交全部字段：协议和地址为空表示用厂商的默认值。保存的 Key 不在表单里，由 Edit 决定是否保留。
		// 设置页还不能改思考，保留 CLI 设的关闭思考；新厂商不支持关闭时恢复默认，不让保存失败。
		if err := ai.Edit(name, func(p *config.AIProfile) {
			key, thinking := p.APIKey, p.Thinking
			*p = in.profile()
			p.APIKey = key
			if thinking == config.AIThinkingOff {
				if p.ThinkingOffSupported() {
					p.Thinking = thinking
				} else {
					thinkingReset = true
				}
			}
		}); err != nil {
			return "", err
		}
		var notes []string
		if hadKey && ai.Profiles[name].APIKey == "" {
			notes = append(notes, "厂商或地址变了，原来保存的 Key 不会发给新地址，已删除，请重新设置。")
		}
		if thinkingReset {
			notes = append(notes, "新的厂商、协议或地址不支持关闭思考，已恢复默认。")
		}
		return strings.Join(notes, ""), nil
	})
}

func (s *desktopServer) handleAIRemoveProfile(w http.ResponseWriter, r *http.Request) {
	if !s.checkAIOrigin(w, r) {
		return
	}
	name := r.PathValue("name")
	s.changeAI(w, r, func(ai *config.AIConfig) (string, error) {
		hadKey, wasEnabled := ai.Profiles[name].APIKey != "", ai.Enabled
		if err := ai.Remove(name); err != nil {
			return "", err
		}
		var notices []string
		if hadKey {
			notices = append(notices, "保存的 Key 也一并删除。")
		}
		if len(ai.Profiles) == 0 && wasEnabled {
			notices = append(notices, "已经没有模型配置，AI 功能已关闭。")
		}
		return strings.Join(notices, ""), nil
	})
}

func (s *desktopServer) handleAIDefault(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !s.readAIRequest(w, r, &in) {
		return
	}
	s.changeAI(w, r, func(ai *config.AIConfig) (string, error) { return "", ai.Use(in.Name) })
}

func (s *desktopServer) handleAIEnabled(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if !s.readAIRequest(w, r, &in) {
		return
	}
	s.changeAI(w, r, func(ai *config.AIConfig) (string, error) { return "", ai.SetEnabled(in.Enabled) })
}

// handleAISetKey 加密保存一个配置的 Key，规则和 sec config ai set-key 一致。Key 只提交不返回，不写日志，错误信息里也不回显。
func (s *desktopServer) handleAISetKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key string `json:"key"`
	}
	if !s.readAIRequest(w, r, &in) {
		return
	}
	secret, err := s.aiSecret()
	if err != nil {
		slog.ErrorContext(r.Context(), "保存 Key 失败", "error", err)
		writeError(w, http.StatusInternalServerError, "保存 Key 失败")
		return
	}
	name := r.PathValue("name")
	before, beforeErr := os.Stat(secret.Path())
	s.changeAI(w, r, func(ai *config.AIConfig) (string, error) {
		clearedEnv, err := ai.SetKey(secret, name, in.Key)
		switch {
		case errors.Is(err, config.ErrInvalidKey), errors.Is(err, config.ErrAIProfileNotFound):
			return "", err
		case err != nil:
			return "", aiInternalError{err}
		}
		var notices []string
		if clearedEnv != "" {
			notices = append(notices, "已不再从环境变量 "+clearedEnv+" 读取 Key。")
		}
		// 第一次生成或替换了损坏的密钥时提醒；替换是改名，和原来不是同一个文件。
		after, err := os.Stat(secret.Path())
		switch {
		case err != nil:
		case beforeErr != nil:
			notices = append(notices, "已生成加密密钥 "+secret.Path()+"，不要和 config.json 一起同步或备份。")
		case !os.SameFile(before, after):
			// 原来的密钥无法使用（损坏等）被换掉了：用它加密的其他 Key 再也解不开，要说清楚。
			var lost []string
			for _, other := range ai.Names() {
				if other != name && ai.Profiles[other].APIKey != "" {
					lost = append(lost, other)
				}
			}
			notice := "原来的加密密钥无法使用，已换成新的 " + secret.Path() + "，不要和 config.json 一起同步或备份。"
			if len(lost) > 0 {
				notice += strings.Join(lost, "、") + " 保存的 Key 无法再解密，请重新设置。"
			}
			notices = append(notices, notice)
		}
		return strings.Join(notices, ""), nil
	})
}

// handleAIUnsetKey 删除一个配置保存的 Key。删除后可能还会从环境变量读到 Key，列表里的 Key 状态会说明。
func (s *desktopServer) handleAIUnsetKey(w http.ResponseWriter, r *http.Request) {
	if !s.checkAIOrigin(w, r) {
		return
	}
	secret, err := s.aiSecret()
	if err != nil {
		slog.ErrorContext(r.Context(), "清除 Key 失败", "error", err)
		writeError(w, http.StatusInternalServerError, "清除 Key 失败")
		return
	}
	name := r.PathValue("name")
	s.changeAI(w, r, func(ai *config.AIConfig) (string, error) {
		had, err := ai.UnsetKey(name)
		if err != nil {
			return "", err
		}
		notice := "已删除 " + name + " 保存的 Key。"
		if !had {
			notice = name + " 没有保存的 Key。"
		}
		// 保存 Key 时已经清空了 api_key_env，删除后不会回到原来的环境变量，说清楚现在用哪个 Key。
		return notice + "现在的 Key：" + ai.KeyStatus(secret, name, os.Getenv) + "。", nil
	})
}

// handleAITestProfile 测试一个配置的连接和工具调用，和 sec config ai test 共用 llm.ProbeProfile。
// 只发固定的测试文字，不含用户数据，AI 功能没有启用时也可以测试。测试失败也返回 200，原因和建议在结果里。
func (s *desktopServer) handleAITestProfile(w http.ResponseWriter, r *http.Request) {
	// 测试会向厂商发请求，不允许别的网页触发。
	if !s.checkAIOrigin(w, r) {
		return
	}
	ctx := r.Context()
	path, err := s.configPath()
	if err != nil {
		slog.ErrorContext(ctx, "读取模型配置失败", "error", err)
		writeError(w, http.StatusInternalServerError, "读取模型配置失败")
		return
	}
	cfg, err := config.LoadFile(ctx, path)
	switch {
	case errors.Is(err, config.ErrInvalidAI), errors.Is(err, config.ErrInvalidJSON):
		writeError(w, http.StatusConflict, "读取模型配置有问题，请先手动修正配置文件："+err.Error())
		return
	case errors.Is(err, config.ErrInvalid):
	case err != nil:
		slog.ErrorContext(ctx, "读取模型配置失败", "error", err)
		writeError(w, http.StatusInternalServerError, "读取模型配置失败")
		return
	}
	name := r.PathValue("name")
	if !cfg.AI.Has(name) {
		writeError(w, http.StatusNotFound, config.ErrAIProfileNotFound.Error()+"："+name)
		return
	}
	secret, err := s.aiSecret()
	if err != nil {
		slog.ErrorContext(ctx, "读取模型配置失败", "error", err)
		writeError(w, http.StatusInternalServerError, "读取模型配置失败")
		return
	}
	writeJSON(w, llm.ProbeProfile(ctx, &cfg.AI, name, secret, os.Getenv))
}

func (in aiProfileInput) profile() config.AIProfile {
	return config.AIProfile{Provider: in.Provider, API: in.API, BaseURL: in.BaseURL, Model: in.Model, APIKeyEnv: in.APIKeyEnv}
}

// aiInternalError 标记 change 里不是用户输入造成的错误（如加密或写 secret.key 失败），changeAI 记日志并返回 500。
type aiInternalError struct{ err error }

func (e aiInternalError) Error() string { return e.err.Error() }
func (e aiInternalError) Unwrap() error { return e.err }

// changeAI 在共享配置的锁里修改 ai 段，返回修改后的模型配置。change 的错误默认是用户输入的问题（名字重复、地址无效等），
// 返回 400；用 aiInternalError 包起来的返回 500。
func (s *desktopServer) changeAI(w http.ResponseWriter, r *http.Request, change func(*config.AIConfig) (string, error)) {
	ctx := r.Context()
	path, err := s.configPath()
	if err != nil {
		slog.ErrorContext(ctx, "修改模型配置失败", "error", err)
		writeError(w, http.StatusInternalServerError, "修改模型配置失败")
		return
	}
	var notice string
	var inputErr error
	// 整个文件不是 JSON 或 ai 段无效时，UpdateAIFile 在文件锁里拒绝修改，不会从默认值开始写、丢掉文件里的 Key。
	err = config.UpdateAIFile(ctx, path, func(ai *config.AIConfig) error {
		notice, inputErr = change(ai)
		return inputErr
	})
	var internal aiInternalError
	switch {
	case errors.As(inputErr, &internal):
		slog.ErrorContext(ctx, "修改模型配置失败", "error", internal.err)
		// 原因（如 secret.key 权限要改为 600）对用户有用，保留在提示里。
		writeError(w, http.StatusInternalServerError, "修改模型配置失败："+internal.err.Error())
		return
	case inputErr != nil:
		writeError(w, http.StatusBadRequest, inputErr.Error())
		return
	case errors.Is(err, config.ErrInvalid):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		slog.ErrorContext(ctx, "修改模型配置失败", "error", err)
		writeError(w, http.StatusInternalServerError, "修改模型配置失败，请稍后重试")
		return
	}
	settings, err := s.loadAISettings(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "读取模型配置失败", "error", err)
		writeError(w, http.StatusInternalServerError, "已修改，重新读取模型配置失败，请刷新")
		return
	}
	settings.Notice = notice
	writeJSON(w, settings)
}

// checkAIOrigin 拒绝别的网页发来的修改请求，和工作台一致。
func (s *desktopServer) checkAIOrigin(w http.ResponseWriter, r *http.Request) bool {
	if !s.dev && r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+r.Host {
		writeError(w, http.StatusForbidden, "不允许跨站修改模型配置")
		return false
	}
	return true
}

// readAIRequest 读取单个 JSON 对象。JSON 内容类型和同源校验阻止跨站表单或网页修改模型配置。
func (s *desktopServer) readAIRequest(w http.ResponseWriter, r *http.Request, value any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "请使用 JSON 格式")
		return false
	}
	if !s.checkAIOrigin(w, r) {
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "模型配置格式无效")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "请求必须包含单个 JSON 对象")
		return false
	}
	return true
}

// aiProblem 从读取配置的错误里取出 ai 段的那一条，邮箱等其他配置项的问题不显示在 AI 设置里。
func aiProblem(err error) string {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, e := range joined.Unwrap() {
			if errors.Is(e, config.ErrInvalidAI) {
				return e.Error()
			}
		}
	}
	return err.Error()
}
