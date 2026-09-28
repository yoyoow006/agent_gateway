package agent

import (
	"bytes"

	"github.com/BurntSushi/toml"
)

func tomlDecode(data []byte, out any) (any, error) {
	meta, err := toml.NewDecoder(bytes.NewReader(data)).Decode(out)
	return meta, err
}
