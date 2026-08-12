// Package api 实现 daemon api socket 的应用层：解析 JSON 请求，经 state.Core
// 读写 AppState（Rule 1：绝不直接改 state，只走 Core.Send/Snapshot），组装 JSON 响应。
//
// 这是 Rule 4 双 socket 中 JSON API socket（agent/CLI 用）的语义层。daemon 只负责
// 传输（framing + 握手），把 payload 交给本包；payload 的 method 集合与 schema 在此定义。
//
// 消息格式（纯 JSON，Rule 4 禁 protobuf/gob/base64-in-JSON）：扁平 Request + Response，
// 字段按 method 取所需，短字段名 + omitempty（参考 internal/protocol 的 Hello/Welcome 风格）。
//
// 写操作是 fire-and-forget（Core.Send 阻塞到核心接收即返回）：Response{OK:true} 仅表示
// 投递成功，reduce 的 ErrNotFound/ErrDuplicate 不经 API 反馈（Core 内 log）；客户端靠
// 后续 snapshot 验证。同步写反馈（SendSync）留给后续批次。
package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/su1feng/Vagabond/internal/state"
)

// Request 是 api socket 的客户端请求（JSON）。字段按 method 取所需：
//   - id        新建目标 id（add-workspace / new-tab / new-pane）
//   - workspace/tab/pane  定位父级，或 remove/close 的目标，或 set-focus 的焦点
//   - cwd       new-pane / set-pane-cwd 的工作目录
//   - agent     set-pane-agent 的 agent 会话引用
type Request struct {
	Method    string `json:"method"`
	ID        string `json:"id,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Tab       string `json:"tab,omitempty"`
	Pane      string `json:"pane,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	Agent     string `json:"agent,omitempty"`
}

// Response 是 api socket 的服务端响应（JSON）。
type Response struct {
	OK    bool            `json:"ok"`
	State *state.AppState `json:"state,omitempty"` // snapshot 的结果
	Error string          `json:"error,omitempty"` // 解析错误 / 未知 method / snapshot 失败
}

// Handler 处理 api socket 的 JSON 请求，经 Core 读写 AppState。
type Handler struct {
	core *state.Core
}

// New 创建绑定到 core 的 Handler。
func New(core *state.Core) *Handler {
	return &Handler{core: core}
}

// Handle 解析 payload、路由到 Core、返回 JSON 响应字节。
// 返回 err 仅在序列化失败等严重情况（调用方应关连接）；业务错误（坏 JSON、未知 method、
// snapshot 失败）都进 Response.Error 正常返回。
func (h *Handler) Handle(ctx context.Context, payload []byte) ([]byte, error) {
	var req Request
	if err := json.Unmarshal(payload, &req); err != nil {
		return h.encode(Response{OK: false, Error: fmt.Sprintf("invalid request: %v", err)})
	}
	return h.encode(h.dispatch(ctx, req))
}

func (h *Handler) encode(r Response) ([]byte, error) {
	return json.Marshal(r)
}

// dispatch 把 Request 路由到 Core。写操作 fire-and-forget；snapshot 走 Core.Snapshot。
func (h *Handler) dispatch(ctx context.Context, req Request) Response {
	switch req.Method {
	case "snapshot":
		s, err := h.core.Snapshot(ctx)
		if err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, State: s}
	case "add-workspace":
		h.core.Send(state.AddWorkspace{ID: req.ID})
		return Response{OK: true}
	case "remove-workspace":
		h.core.Send(state.RemoveWorkspace{ID: req.Workspace})
		return Response{OK: true}
	case "new-tab":
		h.core.Send(state.NewTab{WorkspaceID: req.Workspace, ID: req.ID})
		return Response{OK: true}
	case "close-tab":
		h.core.Send(state.CloseTab{WorkspaceID: req.Workspace, ID: req.Tab})
		return Response{OK: true}
	case "new-pane":
		h.core.Send(state.NewPane{WorkspaceID: req.Workspace, TabID: req.Tab, ID: req.ID, Cwd: req.Cwd})
		return Response{OK: true}
	case "close-pane":
		h.core.Send(state.ClosePane{WorkspaceID: req.Workspace, TabID: req.Tab, ID: req.Pane})
		return Response{OK: true}
	case "set-pane-cwd":
		h.core.Send(state.SetPaneCwd{WorkspaceID: req.Workspace, TabID: req.Tab, PaneID: req.Pane, Cwd: req.Cwd})
		return Response{OK: true}
	case "set-pane-agent":
		h.core.Send(state.SetPaneAgent{WorkspaceID: req.Workspace, TabID: req.Tab, PaneID: req.Pane, AgentRef: req.Agent})
		return Response{OK: true}
	case "set-focus":
		h.core.Send(state.SetFocus{Focus: state.Focus{WorkspaceID: req.Workspace, TabID: req.Tab, PaneID: req.Pane}})
		return Response{OK: true}
	default:
		return Response{OK: false, Error: "unknown method: " + req.Method}
	}
}
