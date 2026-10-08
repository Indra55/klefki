//go:build onnx

package semantic

import (
	"context"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/pretrained"
	ort "github.com/yalue/onnxruntime_go"
)

var environment sync.Mutex

type nativeModel struct {
	mu        sync.Mutex
	tokenizer *tokenizer.Tokenizer
	session   *ort.DynamicAdvancedSession
}

func Supported() bool {
	return runtime.GOARCH == "amd64" && (runtime.GOOS == "linux" || runtime.GOOS == "windows")
}

func Open(root string) (Embedder, error) {
	if !Supported() {
		return nil, ErrUnavailable
	}
	if err := Verify(root); err != nil {
		return nil, err
	}
	environment.Lock()
	defer environment.Unlock()
	if !ort.IsInitialized() {
		name := "libonnxruntime.so.1.23.2"
		if runtime.GOOS == "windows" {
			name = "onnxruntime.dll"
		}
		ort.SetSharedLibraryPath(filepath.Join(root, "runtime", runtime.GOOS+"-"+runtime.GOARCH, name))
		if err := ort.InitializeEnvironment(ort.WithLogLevelFatal()); err != nil {
			return nil, ErrUnavailable
		}
	}
	t, err := pretrained.FromFile(filepath.Join(root, "tokenizer.json"))
	if err != nil {
		return nil, ErrModel
	}
	t.WithTruncation(nil)
	t.WithPadding(nil)
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, ErrUnavailable
	}
	defer opts.Destroy()
	if opts.SetIntraOpNumThreads(2) != nil || opts.SetInterOpNumThreads(1) != nil {
		return nil, ErrUnavailable
	}
	s, err := ort.NewDynamicAdvancedSession(filepath.Join(root, "onnx", "model.onnx"), []string{"input_ids", "attention_mask", "token_type_ids"}, []string{"last_hidden_state"}, opts)
	if err != nil {
		return nil, ErrModel
	}
	return &nativeModel{tokenizer: t, session: s}, nil
}

func (m *nativeModel) Fingerprint() string { return Fingerprint() }
func (m *nativeModel) Tokens(text string) ([]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session == nil {
		return nil, ErrUnavailable
	}
	if len(text) > 2<<20 {
		return nil, ErrLimit
	}
	en, err := m.tokenizer.EncodeSingle(text, false)
	if err != nil {
		return nil, ErrModel
	}
	out := make([]int64, len(en.Ids))
	for i, id := range en.Ids {
		out[i] = int64(id)
	}
	return out, nil
}

func (m *nativeModel) EmbedTokens(ctx context.Context, tokens []int64) ([]float32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.session == nil {
		return nil, ErrUnavailable
	}
	if len(tokens) > MaxTokens {
		return nil, ErrLimit
	}
	ids := make([]int64, len(tokens)+2)
	ids[0] = 101
	copy(ids[1:], tokens)
	ids[len(ids)-1] = 102
	mask, types := make([]int64, len(ids)), make([]int64, len(ids))
	for i := range mask {
		mask[i] = 1
	}
	defer clear(ids)
	defer clear(mask)
	defer clear(types)
	inputs := []ort.Value{}
	defer func() {
		for _, v := range inputs {
			v.Destroy()
		}
	}()
	for _, data := range [][]int64{ids, mask, types} {
		t, err := ort.NewTensor(ort.NewShape(1, int64(len(ids))), data)
		if err != nil {
			return nil, ErrModel
		}
		inputs = append(inputs, t)
	}
	outputs := []ort.Value{nil}
	defer func() {
		for _, v := range outputs {
			if v != nil {
				if t, ok := v.(*ort.Tensor[float32]); ok {
					clear(t.GetData())
				}
				v.Destroy()
			}
		}
	}()
	opts, err := ort.NewRunOptions()
	if err != nil {
		return nil, ErrModel
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			opts.Terminate()
		case <-stop:
		}
	}()
	err = m.session.RunWithOptions(inputs, outputs, opts)
	close(stop)
	<-done
	opts.Destroy()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, ErrModel
	}
	t, ok := outputs[0].(*ort.Tensor[float32])
	if !ok || len(t.GetData()) != len(ids)*Dimension {
		return nil, ErrModel
	}
	// BGE uses the first token (CLS), rather than mean pooling over tokens.
	out := append([]float32(nil), t.GetData()[:Dimension]...)
	if !Normalize(out) {
		clear(out)
		return nil, ErrModel
	}
	return out, nil
}

func (m *nativeModel) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session != nil {
		m.session.Destroy()
		m.session = nil
		m.tokenizer = nil
	}
}
