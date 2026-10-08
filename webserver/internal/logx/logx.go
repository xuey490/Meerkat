// Package logx 把 webserver 的日志统一到 zap（复用 pkg/logger 的 zap + lumberjack 装配）。
//
// 设计取舍：
//   - 全仓库既有代码大量使用标准库 log/slog，这里实现一个 slog.Handler 桥接到 zap，
//     而不是逐个替换调用点——既保留「结构化日志」能力，又能把 GORM 日志、业务告警、
//     访问日志统统写进 logs/app.log（按大小轮转、可压缩）。
//   - zap 的 WriteSyncer 同时写控制台与文件，控制台因此多出一份 JSON 行；
//     原有的彩色控制台输出（console.go）保持不变。
package logx

import (
	"context"
	"log/slog"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/company/monitor-webserver/pkg/logger"
)

// DefaultService 是日志中默认的 service 维度。
const DefaultService = "webserver"

// callerSkip 是 slog→zap 桥接需要跳过的栈帧数：
// 0 = zap 调用点（桥接内部），+1 slog.Logger.log，+2 slog.Logger.<Level>，+3 真实调用方。
const callerSkip = 3

// Setup 按配置构建 zap 日志器，并把标准库 slog 的默认输出接管到该日志器。
//
// 参数 Parameters:
//   - cfg (logger.Config): 日志配置（复用 pkg/logger.Config，字段与 webserver.yaml 的 log 段一致）。
//
// 返回 Returns:
//   - log (*zap.Logger): 已装配的 zap 日志器，调用方负责在退出时 Sync。
//   - err (error): 级别非法或目录创建失败时返回非 nil。
func Setup(cfg logger.Config) (*zap.Logger, error) {
	cfg = withDefaults(cfg)
	log, err := logger.New(cfg)
	if err != nil {
		return nil, err
	}
	// 桥接用的实例多跳几帧，让日志里的 caller 指向真正的调用点（业务代码），
	// 而不是桥接实现；返回给调用方的实例保持默认调用深度。
	slog.SetDefault(slog.New(NewHandler(log.WithOptions(zap.AddCallerSkip(callerSkip)))))
	return log, nil
}

// withDefaults 用 pkg/logger 的默认值补齐未配置字段。
//
// 注意 file.enabled 是普通 bool，无法区分「未配置」与「显式关闭」：
// 这里仅当整个 log 段都没写（service / filename / enabled 全为空）时才套用默认值（默认写文件）。
//
// 参数 Parameters:
//   - cfg (logger.Config): 原始配置。
//
// 返回 Returns:
//   - filled (logger.Config): 补齐后的配置。
func withDefaults(cfg logger.Config) logger.Config {
	defaults := logger.DefaultConfig(DefaultService)
	if cfg.Service == "" && cfg.File.Filename == "" && !cfg.File.Enabled {
		cfg = defaults
	}
	if cfg.Service == "" {
		cfg.Service = defaults.Service
	}
	if cfg.Environment == "" {
		cfg.Environment = defaults.Environment
	}
	if cfg.Level == "" {
		cfg.Level = defaults.Level
	}
	if cfg.Encoding == "" {
		cfg.Encoding = defaults.Encoding
	}
	if cfg.File.Filename == "" {
		cfg.File.Filename = defaults.File.Filename
	}
	if cfg.File.MaxSizeMB <= 0 {
		cfg.File.MaxSizeMB = defaults.File.MaxSizeMB
	}
	if cfg.File.MaxBackups <= 0 {
		cfg.File.MaxBackups = defaults.File.MaxBackups
	}
	if cfg.File.MaxAgeDays <= 0 {
		cfg.File.MaxAgeDays = defaults.File.MaxAgeDays
	}
	return cfg
}

// zapHandler 把 slog 记录转发到 zap。
type zapHandler struct {
	// log 目标 zap 日志器。
	log *zap.Logger
	// attrs WithAttrs 累积的固定字段。
	attrs []slog.Attr
}

// NewHandler 创建把 slog 记录写入 zap 的处理器。
//
// 参数 Parameters:
//   - log (*zap.Logger): 目标 zap 日志器。
//
// 返回 Returns:
//   - handler (slog.Handler): slog 处理器。
func NewHandler(log *zap.Logger) slog.Handler { return &zapHandler{log: log} }

// Enabled 始终返回 true，级别过滤交给 zap 自身处理。
//
// 参数 Parameters:
//   - _ (context.Context): 未使用。
//   - _ (slog.Level): 未使用。
//
// 返回 Returns:
//   - enabled (bool): 恒为 true。
func (h *zapHandler) Enabled(context.Context, slog.Level) bool { return true }

// Handle 把一条 slog 记录映射为 zap 的同级日志。
//
// 参数 Parameters:
//   - _ (context.Context): 未使用。
//   - record (slog.Record): slog 记录。
//
// 返回 Returns:
//   - err (error): 恒为 nil（zap 不返回写入错误）。
func (h *zapHandler) Handle(_ context.Context, record slog.Record) error {
	fields := make([]zap.Field, 0, len(h.attrs)+record.NumAttrs())
	for _, attr := range h.attrs {
		fields = append(fields, toField(attr))
	}
	record.Attrs(func(attr slog.Attr) bool {
		fields = append(fields, toField(attr))
		return true
	})
	message := record.Message
	switch {
	case record.Level >= slog.LevelError:
		h.log.Error(message, fields...)
	case record.Level >= slog.LevelWarn:
		h.log.Warn(message, fields...)
	case record.Level >= slog.LevelInfo:
		h.log.Info(message, fields...)
	default:
		h.log.Debug(message, fields...)
	}
	return nil
}

// WithAttrs 返回携带固定字段的新处理器。
//
// 参数 Parameters:
//   - attrs ([]slog.Attr): 追加的固定字段。
//
// 返回 Returns:
//   - handler (slog.Handler): 新处理器。
func (h *zapHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return &zapHandler{log: h.log, attrs: merged}
}

// WithGroup 兼容 slog 的分组语义：zap 侧展开为 `<group>.<key>` 字段名。
//
// 参数 Parameters:
//   - name (string): 分组名。
//
// 返回 Returns:
//   - handler (slog.Handler): 新处理器。
func (h *zapHandler) WithGroup(name string) slog.Handler {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return h
	}
	prefixed := make([]slog.Attr, 0, len(h.attrs))
	for _, attr := range h.attrs {
		attr.Key = trimmed + "." + attr.Key
		prefixed = append(prefixed, attr)
	}
	return &zapHandler{log: h.log, attrs: prefixed}
}

// toField 把 slog.Attr 转换为 zap.Field。
//
// 参数 Parameters:
//   - attr (slog.Attr): slog 字段。
//
// 返回 Returns:
//   - field (zap.Field): zap 字段。
func toField(attr slog.Attr) zap.Field {
	value := attr.Value.Resolve()
	switch value.Kind() {
	case slog.KindString:
		return zap.String(attr.Key, value.String())
	case slog.KindInt64:
		return zap.Int64(attr.Key, value.Int64())
	case slog.KindUint64:
		return zap.Uint64(attr.Key, value.Uint64())
	case slog.KindFloat64:
		return zap.Float64(attr.Key, value.Float64())
	case slog.KindBool:
		return zap.Bool(attr.Key, value.Bool())
	case slog.KindDuration:
		return zap.Duration(attr.Key, value.Duration())
	case slog.KindTime:
		return zap.Time(attr.Key, value.Time())
	case slog.KindGroup:
		return zap.Any(attr.Key, value.Group())
	default:
		if err, ok := value.Any().(error); ok {
			return zap.NamedError(attr.Key, err)
		}
		return zap.Any(attr.Key, value.Any())
	}
}

// LevelOf 把字符串级别转换为 zapcore.Level（非法时返回 info）。
//
// 参数 Parameters:
//   - level (string): 级别文本，如 debug / info / warn / error。
//
// 返回 Returns:
//   - parsed (zapcore.Level): 解析结果。
func LevelOf(level string) zapcore.Level {
	parsed := zapcore.InfoLevel
	if err := parsed.Set(strings.TrimSpace(level)); err != nil {
		return zapcore.InfoLevel
	}
	return parsed
}
