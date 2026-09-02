package protocol

// DropHook 在翻译降级（如 cache_control 被丢弃、thinking 无法回传）时被调用；
// nil 表示忽略。网关启动时替换为日志实现。
var DropHook func(detail string)

// NotifyDrop 触发一次降级通知。
func NotifyDrop(detail string) {
	if DropHook != nil {
		DropHook(detail)
	}
}
