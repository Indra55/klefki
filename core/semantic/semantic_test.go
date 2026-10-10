package semantic

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestFieldsRemainEvidenceBacked(t *testing.T) {
	f := Fields("Issued January 7, 2026 and 07/08/2026. INR 1,234.50 and $99.00.")
	if len(f) != 4 || f[0].Value != "2026-01-07" || f[1].Value != "" || f[2].Currency != "INR" || f[2].Value != "1234.50" || f[3].Currency != "" || !ValidFields(f) {
		t.Fatal(f)
	}
	for _, x := range f {
		if x.Kind != "date" && x.Kind != "amount" {
			t.Fatal(x)
		}
	}
}

func TestPinnedModelContract(t *testing.T) {
	m := ModelManifest()
	if m.Model != "BAAI/bge-small-en-v1.5" || m.Dimension != Dimension || m.Window != Window || m.Overlap != Overlap || m.MaxTokens != MaxTokens || m.Pooling != "cls-l2-v1" {
		t.Fatal("model and inference contract diverged")
	}
	if Fingerprint() == "1d6c477d1aed0bf27f65a3d05281c29d1735d73840519d5bf7258a17d6638c8f" {
		t.Fatal("MiniLM indexes would remain current")
	}
}
func TestRealModel(t *testing.T) {
	root := os.Getenv("KLEFKI_MODEL_DIR")
	if root == "" || !Supported() {
		if os.Getenv("KLEFKI_REQUIRE_ONNX") == "1" {
			t.Fatal("required model/runtime unavailable")
		}
		t.Skip("set KLEFKI_MODEL_DIR and build with -tags onnx")
	}
	m, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	a, e := Embed(context.Background(), m, "A national identity card proves who I am.")
	if e != nil {
		t.Fatal(e)
	}
	b, e := Embed(context.Background(), m, "Personal identification document.")
	if e != nil {
		t.Fatal(e)
	}
	c, e := Embed(context.Background(), m, "The electric utility charged for power consumption.")
	if e != nil {
		t.Fatal(e)
	}
	if !ValidVector(a) || Cosine(a, b) <= Cosine(a, c) {
		t.Fatal("real similarity")
	}
	tokens, e := m.Tokens(strings.Repeat("electricity ", 300))
	if e != nil || len(tokens) <= 254 || len(tokens) > MaxTokens {
		t.Fatal("BGE context tokens", e)
	}
	long, e := m.EmbedTokens(context.Background(), tokens)
	if e != nil || !ValidVector(long) {
		t.Fatal("BGE context inference", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = m.EmbedTokens(ctx, []int64{100}); e == nil {
		t.Fatal("canceled inference ran")
	}
}
