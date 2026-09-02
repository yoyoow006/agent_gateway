package protocol

import (
	"crypto/rand"
	"encoding/hex"
)

// NewID 生成 <prefix>_<24hex> 随机 ID（如 msg_ / fc_ / resp_）。
func NewID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b)
}
