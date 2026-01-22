// Copyright The AIGW Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package types

import (
	"encoding/json"
	"fmt"
)

// LLMErrorResponse 表示 LLM 服务的错误响应
type LLMErrorResponse struct {
	Object  string `json:"object,omitempty"`
	Message string `json:"message,omitempty"`
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
}

// LLMLogItems LLM 日志项
type LLMLogItems struct {
	// ModelName 模型名称
	ModelName string `json:"model_name,omitempty"`
	// InputTokens 输入 token 数
	InputTokens int `json:"input_tokens,omitempty"`
	// OutputTokens 输出 token 数
	OutputTokens int `json:"output_tokens,omitempty"`
	// ErrorMessage 错误消息
	ErrorMessage string `json:"error_message,omitempty"`
}

// SetErrorMessage 设置错误消息
func (l *LLMLogItems) SetErrorMessage(msg string) {
	l.ErrorMessage = msg
}

// ErrCode 错误码定义
type ErrCode struct {
	Code int    `json:"code"`
	Type string `json:"type"`
	Msg  string `json:"msg"`
}

// 预定义错误码
var (
	ErrBadRequest = ErrCode{
		Code: 400,
		Type: "bad_request",
		Msg:  "Bad Request",
	}
	ErrNotFound = ErrCode{
		Code: 404,
		Type: "not_found",
		Msg:  "Not Found",
	}
	ErrInferenceServer = ErrCode{
		Code: 503,
		Type: "inference_server_error",
		Msg:  "Inference Server Error",
	}
)

// GatewayErrorResponse 网关错误响应
type GatewayErrorResponse struct {
	Error   *GatewayError `json:"error"`
	TraceID string        `json:"trace_id,omitempty"`
}

// GatewayError 网关错误详情
type GatewayError struct {
	Code    int    `json:"code"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

// FormatGatewayResponse 格式化网关错误响应
func FormatGatewayResponse(errCode *ErrCode, traceID, message string) []byte {
	resp := &GatewayErrorResponse{
		Error: &GatewayError{
			Code:    errCode.Code,
			Type:    errCode.Type,
			Message: message,
		},
		TraceID: traceID,
	}
	data, _ := json.Marshal(resp)
	return data
}

// String 返回 ErrCode 的字符串表示
func (e *ErrCode) String() string {
	return fmt.Sprintf("code=%d, type=%s, msg=%s", e.Code, e.Type, e.Msg)
}
