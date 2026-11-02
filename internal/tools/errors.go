package tools

import (
	"encoding/json"
	"errors"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

// ErrorCode — код типизированной ошибки тула.
type ErrorCode string

const (
	CodeAccessDenied  ErrorCode = "access_denied"
	CodeNotFound      ErrorCode = "not_found"
	CodeInvalidArg    ErrorCode = "invalid_argument"
	CodeConflict      ErrorCode = "conflict"
	CodeUnavailable   ErrorCode = "unavailable"
	CodeInternalError ErrorCode = "internal"
)

// ToolError — единая ошибка тула: ago превращает её в ToolErrorEvent, модель
// видит code/retryable/remediation и может исправиться; игроку — русское
// сообщение.
type ToolError struct {
	Code        ErrorCode `json:"code"`
	Retryable   bool      `json:"retryable"`
	Remediation string    `json:"remediation"`
	Message     string    `json:"error"`
}

func (e *ToolError) Error() string {
	data, err := json.Marshal(map[string]any{
		"ok":          false,
		"code":        e.Code,
		"retryable":   e.Retryable,
		"remediation": e.Remediation,
		"error":       e.Message,
	})
	if err != nil {
		return `{"ok":false,"code":"internal","error":"tool error"}`
	}
	return string(data)
}

func toolError(code ErrorCode, retryable bool, message, remediation string) *ToolError {
	return &ToolError{Code: code, Retryable: retryable, Remediation: remediation, Message: message}
}

func errNoCampaign() *ToolError {
	return toolError(CodeAccessDenied, false,
		"В этом чате нет привязанной кампании.",
		"Start a campaign in this chat first (start_campaign) or use a chat bound to the campaign.")
}

func errNoIdentity() *ToolError {
	return toolError(CodeAccessDenied, false,
		"Не удалось определить кто вы.",
		"Retry from the bound Telegram chat; the tool needs the caller's user id.")
}

func errNotMember() *ToolError {
	return toolError(CodeAccessDenied, false,
		"Вы не участник этой кампании.",
		"Ask the campaign DM to invite you (invite_member) or join with /join in the campaign chat.")
}

func errDMOnly() *ToolError {
	return toolError(CodeAccessDenied, false,
		"Это действие доступно только администратору (dm) кампании.",
		"Ask the campaign DM to perform this administrative action.")
}

func errNotFound(message, remediation string) *ToolError {
	return toolError(CodeNotFound, false, message, remediation)
}

func errInvalid(message, remediation string) *ToolError {
	return toolError(CodeInvalidArg, false, message, remediation)
}

func errConflict(message, remediation string) *ToolError {
	return toolError(CodeConflict, false, message, remediation)
}

func errUnavailable(message, remediation string) *ToolError {
	return toolError(CodeUnavailable, true, message, remediation)
}

func errInternal(err error) *ToolError {
	return toolError(CodeInternalError, true, err.Error(),
		"Retry once; if the error persists report it to the operator.")
}

// fromStoreError переводит ошибку хранилища в типизированную ошибку тула.
func fromStoreError(err error) error {
	if err == nil {
		return nil
	}
	var storeErr *store.StoreError
	if !errors.As(err, &storeErr) {
		return errInternal(err)
	}
	switch storeErr.Code {
	case store.ErrAccessDenied:
		return toolError(CodeAccessDenied, false, storeErr.Message,
			"Check the campaign membership or the DM role; do not retry with the same identity.")
	case store.ErrNotFound:
		return toolError(CodeNotFound, false, storeErr.Message,
			"Check the name or slug with the matching list tool and retry with a valid value.")
	case store.ErrDuplicate:
		return toolError(CodeConflict, false, storeErr.Message,
			"Use a different name or update the existing entity instead of creating a duplicate.")
	case store.ErrConflict:
		return toolError(CodeConflict, false, storeErr.Message,
			"Read the current state with a read tool and retry with a valid transition.")
	case store.ErrPartyFull:
		return toolError(CodeConflict, false, storeErr.Message,
			"The party is full; remove a character or start a new campaign.")
	default:
		return toolError(CodeInternalError, true, storeErr.Message,
			"Retry once; if the error persists report it to the operator.")
	}
}
