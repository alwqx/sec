//go:build windows || darwin

package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/alwqx/sec/config"
)

// aiSettings 是设置页「AI 模型」读取的模型配置，和 sec config ai list --json 一致，Key 只给来源和末 4 位。
type aiSettings struct {
	Enabled  bool                   `json:"enabled"`
	Default  string                 `json:"default"`
	Profiles []config.AIProfileView `json:"profiles"`
	// Invalid 是 ai 段无效的原因：这时按没有配置显示，提示用户手动修正。
	Invalid string `json:"invalid,omitempty"`
}

// aiSecret 返回加密 Key 用的 secret.key；测试时放在工作台文件旁边，和共享配置一样。
func (s *desktopServer) aiSecret() (*config.AISecret, error) {
	if s.workspace.path != "" {
		return config.NewAISecret(filepath.Join(filepath.Dir(s.workspace.path), "secret.key")), nil
	}
	return config.DefaultAISecret()
}

func (s *desktopServer) handleAISettings(w http.ResponseWriter, r *http.Request) {
	path, err := s.configPath()
	if err != nil {
		slog.ErrorContext(r.Context(), "读取模型配置失败", "error", err)
		writeError(w, http.StatusInternalServerError, "读取模型配置失败")
		return
	}
	cfg, err := config.LoadFile(r.Context(), path)
	var invalid string
	switch {
	case errors.Is(err, config.ErrInvalidAI):
		invalid = aiProblem(err)
	// 整个文件无效时 ai 段也换成了默认值，不能显示「还没有模型配置」：照着 sec config ai add 会覆盖文件、丢掉已存的 Key。
	case errors.Is(err, config.ErrInvalidJSON):
		invalid = err.Error() + "；读不到模型配置"
	case errors.Is(err, config.ErrInvalid):
		slog.WarnContext(r.Context(), "共享配置无效，使用默认值", "error", err)
	case err != nil:
		slog.ErrorContext(r.Context(), "读取模型配置失败", "error", err)
		writeError(w, http.StatusInternalServerError, "读取模型配置失败，请检查 ~/.sec/config.json")
		return
	}
	secret, err := s.aiSecret()
	if err != nil {
		slog.ErrorContext(r.Context(), "读取模型配置失败", "error", err)
		writeError(w, http.StatusInternalServerError, "读取模型配置失败")
		return
	}
	ai := cfg.AI
	writeJSON(w, aiSettings{Enabled: ai.Enabled, Default: ai.Default, Profiles: ai.Views(secret, os.Getenv), Invalid: invalid})
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
