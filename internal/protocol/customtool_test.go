package protocol

import "testing"

func TestUnwrapCustomInput(t *testing.T) {
	if got := UnwrapCustomInput(`{"code":"return 1;"}`); got != "return 1;" {
		t.Errorf("code 解包 = %q", got)
	}
	// 模型未按 schema：整段文本兜底
	if got := UnwrapCustomInput(`裸文本`); got != `裸文本` {
		t.Errorf("兜底 = %q", got)
	}
	// code 键存在但值为空串：应返回空文本而非字面量 JSON（审查 F1）
	if got := UnwrapCustomInput(`{"code":""}`); got != "" {
		t.Errorf("空 code = %q（应为空串）", got)
	}
}
