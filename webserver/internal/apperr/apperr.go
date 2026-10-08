// Package apperr 定义跨层传递的业务错误。
//
// 设计要点：
//   - 本包不依赖 gin / gorm，logic、service 均可直接返回这些错误；
//   - HTTP 状态码与业务错误码的映射只在 api 层做一次（见 api/v1/response.go），
//     避免各处硬编码 status 与 code；
//   - 通过 errors.Is + KindOf 判定错误类型，包装链上的原因（cause）保留原始错误，
//     便于日志与排查。
package apperr

import (
	"errors"
	"fmt"
)

// Kind 表示业务错误类别，用于统一映射 HTTP 状态码与业务错误码。
type Kind int

const (
	// KindInternal 内部错误：500 + B0001。
	KindInternal Kind = iota
	// KindInvalid 参数错误：400 + A0001。
	KindInvalid
	// KindNotFound 资源不存在：404 + A0004。
	KindNotFound
	// KindConflict 资源冲突：409 + A0002。
	KindConflict
	// KindForbidden 权限不足：403 + A0301。
	KindForbidden
	// KindUnauthorized 未认证：401 + A0230。
	KindUnauthorized
	// KindUnavailable 依赖不可用：502 + B0201。
	KindUnavailable
)

// Error 是带类别的业务错误。
type Error struct {
	// kind 错误类别。
	kind Kind
	// message 面向用户的提示信息。
	message string
	// cause 底层原因，可为空。
	cause error
}

// Error 实现 error 接口，输出「提示信息: 底层原因」。
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.message, e.cause)
	}
	return e.message
}

// Unwrap 返回底层原因，供 errors.Is / errors.As 穿透。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Kind 返回错误类别。
func (e *Error) Kind() Kind {
	if e == nil {
		return KindInternal
	}
	return e.kind
}

// Message 返回面向用户的提示信息。
func (e *Error) Message() string {
	if e == nil {
		return ""
	}
	return e.message
}

// New 构造指定类别的业务错误。
//
// 参数 Parameters:
//   - kind (Kind): 错误类别。
//   - message (string): 面向用户的提示信息。
//
// 返回 Returns:
//   - err (*Error): 业务错误。
func New(kind Kind, message string) *Error {
	return &Error{kind: kind, message: message}
}

// Wrap 在业务错误上附加底层原因。
//
// 参数 Parameters:
//   - kind (Kind): 错误类别。
//   - message (string): 面向用户的提示信息。
//   - cause (error): 底层原因，可为 nil。
//
// 返回 Returns:
//   - err (*Error): 携带原因的业务错误。
func Wrap(kind Kind, message string, cause error) *Error {
	return &Error{kind: kind, message: message, cause: cause}
}

// Invalid 构造参数错误（400 + A0001）。
func Invalid(message string) *Error { return New(KindInvalid, message) }

// NotFound 构造资源不存在错误（404 + A0004）。
func NotFound(message string) *Error { return New(KindNotFound, message) }

// Conflict 构造资源冲突错误（409 + A0002）。
func Conflict(message string) *Error { return New(KindConflict, message) }

// Forbidden 构造权限不足错误（403 + A0301）。
func Forbidden(message string) *Error { return New(KindForbidden, message) }

// Unauthorized 构造未认证错误（401 + A0230）。
func Unauthorized(message string) *Error { return New(KindUnauthorized, message) }

// Unavailable 构造依赖不可用错误（502 + B0201）。
func Unavailable(message string, cause error) *Error {
	return Wrap(KindUnavailable, message, cause)
}

// Internal 构造内部错误（500 + B0001）。
func Internal(message string, cause error) *Error { return Wrap(KindInternal, message, cause) }

// KindOf 返回错误的类别；非本包错误一律视为内部错误。
//
// 参数 Parameters:
//   - err (error): 任意错误。
//
// 返回 Returns:
//   - kind (Kind): 命中的类别，未命中返回 KindInternal。
func KindOf(err error) Kind {
	var target *Error
	if errors.As(err, &target) {
		return target.Kind()
	}
	return KindInternal
}

// MessageOf 返回错误的用户可见提示；非本包错误返回 fallback。
//
// 参数 Parameters:
//   - err (error): 任意错误。
//   - fallback (string): 未命中业务错误时的兜底提示。
//
// 返回 Returns:
//   - message (string): 提示信息。
func MessageOf(err error, fallback string) string {
	var target *Error
	if errors.As(err, &target) {
		return target.Message()
	}
	if err == nil {
		return fallback
	}
	return fallback
}
