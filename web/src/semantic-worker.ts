/// <reference lib="webworker" />
import { BertTokenizer } from "@huggingface/transformers";
import * as ort from "onnxruntime-web/wasm";
import manifest from "./model-manifest.json";
const cacheName = "vault-model-" + manifest.fingerprint;
let session: ort.InferenceSession | undefined,
  tokenizer: BertTokenizer | undefined;
async function asset(path: string) {
  const cache = await caches.open(cacheName),
    a = manifest.assets.find((a) => a.path === path);
  if (!a) throw Error("SEMANTIC_MODEL_INVALID");
  const r = await cache.match(a.url);
  if (!r) throw Error("SEMANTIC_UNAVAILABLE");
  const b = await r.arrayBuffer();
  const hash = Array.from(
    new Uint8Array(await crypto.subtle.digest("SHA-256", b)),
    (v) => v.toString(16).padStart(2, "0"),
  ).join("");
  if (b.byteLength !== a.size || hash !== a.sha256)
    throw Error("SEMANTIC_MODEL_INVALID");
  return b;
}
async function ready() {
  if (session) return;
  tokenizer = new BertTokenizer(
    JSON.parse(new TextDecoder().decode(await asset("tokenizer.json"))),
    JSON.parse(new TextDecoder().decode(await asset("tokenizer_config.json"))),
  );
  ort.env.wasm.numThreads = 1;
  ort.env.wasm.wasmPaths = "/models/runtime/";
  ort.env.logLevel = "fatal";
  session = await ort.InferenceSession.create(await asset("onnx/model.onnx"), {
    executionProviders: ["wasm"],
    logSeverityLevel: 4,
  });
}
async function embed(text: string) {
  await ready();
  if (text.length > 4096) throw Error("SEARCH_LIMIT");
  const ids = tokenizer!.encode(text, { add_special_tokens: false });
  if (ids.length > manifest.maxTokens) throw Error("SEARCH_LIMIT");
  const tokens = [101, ...ids, 102],
    n = tokens.length,
    mask = BigInt64Array.from(tokens, () => 1n),
    types = new BigInt64Array(n),
    input = BigInt64Array.from(tokens, BigInt);
  const feeds = {
    input_ids: new ort.Tensor("int64", input, [1, n]),
    attention_mask: new ort.Tensor("int64", mask, [1, n]),
    token_type_ids: new ort.Tensor("int64", types, [1, n]),
  };
  const output = await session!.run(feeds);
  try {
    const hidden = output.last_hidden_state.data as Float32Array;
    if (hidden.length !== n * 384) throw Error("SEMANTIC_MODEL_INVALID");
    const v = hidden.slice(0, 384);
    const norm = Math.sqrt(v.reduce((n, x) => n + x * x, 0));
    if (!Number.isFinite(norm) || norm < 1e-12)
      throw Error("SEMANTIC_MODEL_INVALID");
    for (let i = 0; i < v.length; i++) v[i] /= norm;
    return {
      tokens: ids,
      vector: Array.from(v),
      fingerprint: manifest.fingerprint,
    };
  } finally {
    for (const t of Object.values(output)) {
      if (t.data instanceof Float32Array) t.data.fill(0);
      t.dispose();
    }
    for (const t of Object.values(feeds)) t.dispose();
    input.fill(0n);
    mask.fill(0n);
    types.fill(0n);
    tokens.fill(0);
  }
}
let work = Promise.resolve();
self.onmessage = (e) => {
  const { id, text } = e.data;
  work = work.then(async () => {
    try {
      const result = await embed(text);
      self.postMessage({ id, result });
    } catch (e) {
      self.postMessage({
        id,
        error:
          e instanceof Error && /^(SEMANTIC_|SEARCH_)/.test(e.message)
            ? e.message
            : "SEMANTIC_MODEL_INVALID",
      });
    }
  });
};
