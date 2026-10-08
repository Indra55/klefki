//go:build !onnx

package semantic

func Open(string) (Embedder, error) { return nil, ErrUnavailable }
func Supported() bool               { return false }
