package json

import "github.com/bytedance/sonic"

var (
	json                = sonic.ConfigDefault
	Marshal             = json.Marshal
	Unmarshal           = json.Unmarshal
	UnmarshalFromString = json.UnmarshalFromString
	MarshalIndent       = json.MarshalIndent
	NewDecoder          = json.NewDecoder
	NewEncoder          = json.NewEncoder
)
