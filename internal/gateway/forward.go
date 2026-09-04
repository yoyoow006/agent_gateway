package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"agent_gateway/internal/config"
	"agent_gateway/internal/protocol"
	"agent_gateway/internal/protocol/openairesponses"
)

// maxBody 是 failover 重放缓冲上限。
var maxBody = int64(config.DefaultMaxBodyBytes)

// retryableStatus 报告是否换下一家供应商重试。
// 401/403 也切换：供应商密钥失效时自动绕过（计失败）。
func retryableStatus(code int) bool {
	switch code {
	case 401, 403, 408, 429, 500, 502, 503, 504, 529:
		return true
	}
	return false
}

// handleForward 构造通用转发入口。
func (s *Server) handleForward(clientProto config.Protocol) func(http.ResponseWriter, *http.Request, *config.Profile) {
	return func(w http.ResponseWriter, r *http.Request, profile *config.Profile) {
		if r.Method != http.MethodPost && !(clientProto == config.ProtocolOpenAIResponses && r.Method == http.MethodGet) {
			writeError(w, codecFor(clientProto), 405, "方法不允许（该端点仅支持 POST/GET 按协议而定）")
			return
		}
		var body []byte
		if r.Body != nil {
			data, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
			if err != nil {
				writeError(w, codecFor(clientProto), 400, "读取请求体失败: "+err.Error())
				return
			}
			if int64(len(data)) > maxBody {
				writeError(w, codecFor(clientProto), 413, fmt.Sprintf("请求体超过 %d MiB 缓冲上限，无法故障重放", maxBody>>20))
				return
			}
			body = data
		}
		s.forward(w, r, clientProto, profile, body)
	}
}

// forward 按候选链逐家尝试；首字节前失败换下一家，全败返回最后一次错误。
//
//	请求(body) ──→ 提取 custom 工具名单（仅 responses 客户端）
//	  │
//	  ▼ 按链逐家（熔断打开即跳过）
//	┌─ attempt ──────────────────────────────────────────┐
//	│ 同协议：透传 body（仅改写 model）                      │
//	│ 跨协议：ParseRequest→IR（model_map）→BuildRequest    │
//	└────────────────────────────────────────────────────┘
//	  │ 传输失败/401/403/408/429/5xx/529 → 记失败，换下一家 ↺
//	  │ 4xx 其他 → 原样/翻译回传，不计失败，终止
//	  ▼ 2xx
//	relaySuccess：同协议 copyStream 字节透传；
//	          跨协议 SSE→translateStream（解码→custom 打标→编码），
//	                非流式→ParseResponse→BuildResponse
//	全链失败 → relayError（最后一次真实错误，按客户端协议格式）
func (s *Server) forward(w http.ResponseWriter, r *http.Request, clientProto config.Protocol, profile *config.Profile, body []byte) {
	clientCodec := codecFor(clientProto)
	// Responses 客户端（Codex）的 custom 型工具名单：响应翻译时据此把命中名字的
	// tool_use 还原为 custom_tool_call。随请求提取，无会话状态。
	var customTools map[string]bool
	if clientProto == config.ProtocolOpenAIResponses {
		customTools = openairesponses.ExtractCustomTools(body)
	}
	chain := profile.Chain
	if len(chain) == 0 {
		writeError(w, clientCodec, 503, "档案内没有启用的供应商")
		return
	}
	var (
		lastStatus int // 最后一个返回真实 HTTP 错误的供应商
		lastBody   []byte
		lastHdr    http.Header
		lastProto  config.Protocol
		lastTried  bool
		anyAttempt bool // 有供应商被实际尝试过（含传输失败）
	)
	for i := range chain {
		p := chain[i]
		// GET 拉取仅同协议透传（store=false 时 Codex 不会用到）；
		// 必须在 Allow() 之前跳过，否则会白白消耗半开探针名额
		if r.Method == http.MethodGet && p.Protocol != clientProto {
			continue
		}
		br, _ := s.registry.Breaker(p.Name)
		if br != nil && !br.Allow() {
			s.logger.Printf("[agw] 供应商 %s 熔断打开，跳过", p.Name)
			continue
		}
		anyAttempt = true
		s.registry.RecordRequest(p.Name)
		resp, err := s.attempt(r, clientProto, profile, p, body)
		if err != nil {
			s.registry.RecordFailure(p.Name, err.Error())
			s.logger.Printf("[agw] 供应商 %s 尝试失败: %v", p.Name, err)
			continue // 传输失败无上游响应，不覆盖已有真实错误
		}
		if resp.StatusCode >= 400 {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			if retryableStatus(resp.StatusCode) {
				s.registry.RecordFailure(p.Name, fmt.Sprintf("HTTP %d", resp.StatusCode))
				s.logger.Printf("[agw] 供应商 %s 返回 %d，切换下一家", p.Name, resp.StatusCode)
				lastStatus, lastBody, lastHdr, lastProto, lastTried = resp.StatusCode, errBody, resp.Header.Clone(), p.Protocol, true
				continue
			}
			// 客户端错误：原样/翻译回传，不计供应商失败（探针也算存活）
			s.registry.RecordSuccess(p.Name)
			s.relayError(w, clientProto, p.Protocol, resp.StatusCode, errBody, resp.Header)
			return
		}
		// 成功
		s.registry.RecordSuccess(p.Name)
		s.relaySuccess(w, r, clientProto, p, resp, customTools)
		return
	}
	if lastTried {
		s.relayError(w, clientProto, lastProto, lastStatus, lastBody, lastHdr)
		return
	}
	if anyAttempt {
		s.relayError(w, clientProto, clientProto, 502, nil, nil)
		return
	}
	writeError(w, clientCodec, 503, "全部供应商处于熔断冷却中")
}

// attempt 发起一次上游请求，返回响应；构造失败返回错误。
func (s *Server) attempt(r *http.Request, clientProto config.Protocol, profile *config.Profile, p *config.Provider, body []byte) (*http.Response, error) {
	sameProto := p.Protocol == clientProto
	var reqBody []byte
	path := r.URL.Path
	var extraHeader http.Header

	if sameProto {
		reqBody = body
		if len(body) > 0 && r.Method == http.MethodPost {
			reqBody = applyModelMap(body, effectiveModelMap(profile, p))
		}
	} else {
		if len(body) == 0 {
			return nil, fmt.Errorf("跨协议翻译需要请求体")
		}
		ir, err := codecFor(clientProto).ParseRequest(body)
		if err != nil {
			return nil, fmt.Errorf("解析客户端请求: %w", err)
		}
		if mapped, ok := effectiveModelMap(profile, p)[ir.Model]; ok {
			ir.Model = mapped
		}
		buildPath, hdr, built, err := codecFor(p.Protocol).BuildRequest(ir)
		if err != nil {
			return nil, fmt.Errorf("构建 %s 请求: %w", p.Protocol, err)
		}
		path, extraHeader, reqBody = buildPath, hdr, built
	}

	url := strings.TrimSuffix(p.BaseURL, "/") + path
	if r.URL.RawQuery != "" {
		url += "?" + r.URL.RawQuery
	}
	method := r.Method
	var rdr io.Reader
	if len(reqBody) > 0 {
		rdr = bytes.NewReader(reqBody)
	}
	upReq, err := http.NewRequestWithContext(r.Context(), method, url, rdr)
	if err != nil {
		return nil, err
	}
	// 头：客户端头（剥 hop-by-hop/认证/压缩），叠加构建头与供应商认证
	copyHeaders(upReq.Header, r.Header)
	stripHopByHop(upReq.Header)
	upReq.Header.Del("Authorization")
	upReq.Header.Del("X-Api-Key")
	upReq.Header.Del("Accept-Encoding")
	if extraHeader != nil {
		for k, vs := range extraHeader {
			for _, v := range vs {
				upReq.Header.Set(k, v)
			}
		}
	}
	key, err := p.ResolveAPIKey()
	if err != nil {
		return nil, err
	}
	if p.Protocol == config.ProtocolAnthropic {
		upReq.Header.Set("X-Api-Key", key)
	} else {
		upReq.Header.Set("Authorization", "Bearer "+key)
	}
	for k, v := range p.Headers {
		upReq.Header.Set(k, v)
	}

	client := s.clientFor(p)
	resp, err := client.Do(upReq)
	if err != nil {
		return nil, classifyTransportError(err)
	}
	return resp, nil
}

// classifyTransportError 把传输层错误转为可读信息。
func classifyTransportError(err error) error {
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return fmt.Errorf("上游超时: %v", ne)
	}
	return fmt.Errorf("上游连接失败: %v", err)
}

// relaySuccess 把 2xx 响应回传客户端：同协议透传，跨协议按需翻译。
func (s *Server) relaySuccess(w http.ResponseWriter, r *http.Request, clientProto config.Protocol, p *config.Provider, resp *http.Response, customTools map[string]bool) {
	defer resp.Body.Close()
	outHeader := w.Header()
	for k, vs := range resp.Header {
		if isHopByHop(k) || k == "Content-Length" || k == "Content-Encoding" {
			continue
		}
		for _, v := range vs {
			outHeader.Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if p.Protocol == clientProto {
		if err := copyStream(w, resp.Body); err != nil {
			s.logger.Printf("[agw] 供应商 %s 透传流中断: %v", p.Name, err)
		}
		return
	}
	clientCodec, provCodec := codecFor(clientProto), codecFor(p.Protocol)
	if isSSE(resp) || r.URL.Query().Get("stream") != "" {
		translateStream(w, provCodec, clientCodec, resp.Body, p.Name, s.logger, customTools)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	ir, err := provCodec.ParseResponse(resp.StatusCode, body)
	if err != nil {
		s.logger.Printf("[agw] 解析上游响应失败（按原文透传）: %v", err)
		w.Write(body)
		return
	}
	markCustomTools(ir.Parts, customTools)
	_, out := clientCodec.BuildResponse(ir)
	w.Write(out)
}

// relayError 把上游错误回传：跨协议时翻译错误体，保留状态码与 Retry-After。
func (s *Server) relayError(w http.ResponseWriter, clientProto, provProto config.Protocol, status int, body []byte, hdr http.Header) {
	clientCodec, provCodec := codecFor(clientProto), codecFor(provProto)
	if hdr != nil {
		if ra := hdr.Get("Retry-After"); ra != "" {
			w.Header().Set("Retry-After", ra)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if clientProto == provProto {
		w.WriteHeader(status)
		if len(body) > 0 {
			w.Write(body)
		}
		return
	}
	if status == 0 {
		status = 502
	}
	msg := "上游不可用"
	if provCodec != nil && len(body) > 0 {
		msg = provCodec.ParseError(status, body)
	}
	w.WriteHeader(status)
	if clientCodec != nil {
		w.Write(clientCodec.BuildError(status, msg))
	}
}

// handleCountTokens 优先转发 anthropic 上游，否则本地估算。
func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request, profile *config.Profile) {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		writeError(w, codecFor(config.ProtocolAnthropic), 400, "读取请求体失败")
		return
	}
	if int64(len(data)) > maxBody {
		writeError(w, codecFor(config.ProtocolAnthropic), 413, fmt.Sprintf("请求体超过 %d MiB 缓冲上限", maxBody>>20))
		return
	}
	body := data
	for i := range profile.Chain {
		p := profile.Chain[i]
		if p.Protocol != config.ProtocolAnthropic {
			continue
		}
		br, _ := s.registry.Breaker(p.Name)
		if br != nil && !br.Allow() {
			continue
		}
		s.registry.RecordRequest(p.Name)
		resp, aerr := s.attempt(r, config.ProtocolAnthropic, profile, p, body)
		if aerr != nil {
			s.registry.RecordFailure(p.Name, aerr.Error())
			continue
		}
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		status := resp.StatusCode
		resp.Body.Close()
		if status == 404 || status == 405 {
			s.registry.RecordSuccess(p.Name)
			continue // 该上游无此端点，试下一家
		}
		if status >= 400 {
			s.registry.RecordFailure(p.Name, fmt.Sprintf("HTTP %d", status))
			continue
		}
		s.registry.RecordSuccess(p.Name)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write(respBody)
		return
	}
	// 本地粗估：字节/4（CJK 混合场景的保守近似）
	estimate := len(body) / 4
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write([]byte(`{"input_tokens":` + strconv.Itoa(estimate) + `}`))
}

// ---- 工具 ----

// effectiveModelMap 合并档案级与供应商级模型映射（供应商优先）。
func effectiveModelMap(profile *config.Profile, p *config.Provider) map[string]string {
	if len(profile.ModelMap) == 0 && len(p.ModelMap) == 0 {
		return nil
	}
	m := map[string]string{}
	for k, v := range profile.ModelMap {
		m[k] = v
	}
	for k, v := range p.ModelMap {
		m[k] = v
	}
	return m
}

// applyModelMap 在不重排其余字节的前提下重写顶层 model 字段。
func applyModelMap(body []byte, m map[string]string) []byte {
	if len(m) == 0 {
		return body
	}
	var top struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &top); err != nil || top.Model == "" {
		return body
	}
	mapped, ok := m[top.Model]
	if !ok {
		return body
	}
	// 定位顶层 "model" 值的精确字节区间并拼接替换
	dec := json.NewDecoder(bytes.NewReader(body))
	if _, err := dec.Token(); err != nil { // '{'
		return body
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return body
		}
		key, _ := keyTok.(string)
		start := dec.InputOffset()
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return body
		}
		if key == "model" {
			valueStart := start + int64(bytes.IndexByte(body[start:], raw[0]))
			valueEnd := valueStart + int64(len(raw))
			repl, _ := json.Marshal(mapped)
			out := make([]byte, 0, len(body)-len(raw)+len(repl))
			out = append(out, body[:valueStart]...)
			out = append(out, repl...)
			out = append(out, body[valueEnd:]...)
			return out
		}
	}
	return body
}

// copyStream 逐块拷贝并即时冲刷（SSE 低延迟）。
func copyStream(dst io.Writer, src io.Reader) error {
	buf := make([]byte, 32*1024)
	flusher, _ := dst.(http.Flusher)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// translateStream 上游 SSE → IR → 客户端 SSE。
// 截断（解码错误或 EOF 无结束事件）输出客户端协议的错误帧并终止，
// 不合成正常收尾——agent 依赖错误信号触发自带重试落到健康供应商。
func translateStream(w io.Writer, provCodec, clientCodec protocol.Codec, body io.Reader, provName string, logger interface{ Printf(string, ...any) }, customTools map[string]bool) {
	dec := provCodec.NewStreamDecoder(body)
	enc := clientCodec.NewStreamEncoder(newFlushWriter(w))
	sawEnd := false
	for {
		ev, err := dec.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Printf("[agw] 供应商 %s 流中断: %v", provName, err)
			_ = enc.Encode(protocol.Event{Kind: protocol.EvStreamError, ErrMessage: "上游流中断: " + err.Error()})
			return
		}
		if ev.Kind == protocol.EvStreamError {
			logger.Printf("[agw] 供应商 %s 流错误事件: %s", provName, ev.ErrMessage)
		}
		if ev.Kind == protocol.EvStreamEnd {
			sawEnd = true
		}
		if ev.Kind == protocol.EvBlockStart && ev.Block.Kind == protocol.KindToolUse && customTools[ev.Block.ToolName] {
			cp := ev.Block
			cp.CustomTool = true
			ev.Block = cp
		}
		if encErr := enc.Encode(ev); encErr != nil {
			return
		}
	}
	if !sawEnd {
		logger.Printf("[agw] 供应商 %s 流提前结束（无结束事件），按截断处理", provName)
		_ = enc.Encode(protocol.Event{Kind: protocol.EvStreamError, ErrMessage: "上游流提前结束（截断）"})
		return
	}
	_ = enc.Finish()
}

// flushWriter 包装 ResponseWriter，每次写后冲刷。
type flushWriter struct {
	w io.Writer
	f http.Flusher
}

func newFlushWriter(w io.Writer) *flushWriter {
	fw := &flushWriter{w: w}
	if f, ok := w.(http.Flusher); ok {
		fw.f = f
	}
	return fw
}

func (fw *flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if fw.f != nil {
		fw.f.Flush()
	}
	return n, err
}

func isSSE(resp *http.Response) bool {
	return strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
}

var hopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

func isHopByHop(k string) bool {
	return hopByHopHeaders[http.CanonicalHeaderKey(k)]
}

func stripHopByHop(h http.Header) {
	if c := h.Get("Connection"); c != "" {
		for _, name := range strings.Split(c, ",") {
			h.Del(strings.TrimSpace(name))
		}
	}
	for k := range hopByHopHeaders {
		h.Del(k)
	}
}

func copyHeaders(dst, src http.Header) {
	for k, vs := range src {
		if isHopByHop(k) {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// clientFor 按供应商超时配置构建/缓存 HTTP 客户端。
func (s *Server) clientFor(p *config.Provider) *http.Client {
	s.mu.RLock()
	existing, ok := s.clients[p.Name]
	s.mu.RUnlock()
	if ok {
		return existing
	}
	connect := 5 * time.Second
	firstByte := 60 * time.Second
	if p.ConnectTimeoutSec > 0 {
		connect = time.Duration(p.ConnectTimeoutSec) * time.Second
	}
	if p.FirstByteTimeout > 0 {
		firstByte = time.Duration(p.FirstByteTimeout) * time.Second
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: connect, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: firstByte,
	}
	client := &http.Client{Transport: transport}
	s.mu.Lock()
	s.clients[p.Name] = client
	s.mu.Unlock()
	return client
}

// markCustomTools 给命中 custom 名单的 tool_use 部件打标（非流式响应路径）。
func markCustomTools(parts []protocol.Part, customTools map[string]bool) {
	if len(customTools) == 0 {
		return
	}
	for i := range parts {
		if parts[i].Kind == protocol.KindToolUse && customTools[parts[i].ToolName] {
			parts[i].CustomTool = true
		}
	}
}
