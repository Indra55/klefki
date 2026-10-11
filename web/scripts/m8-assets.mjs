import { readFile, writeFile, mkdir, rm } from "node:fs/promises";
import { createHash } from "node:crypto";
const manifest = JSON.parse(
  await readFile(new URL("../../core/semantic/manifest.json", import.meta.url)),
);
const prefix = "/models/bge-small/";
const source = process.env.KLEFKI_MODEL_DIR;
const download = process.argv.includes("--download");
const publicRoot = new URL("../public/models/", import.meta.url);
await rm(new URL("minilm/", publicRoot), { recursive: true, force: true });
const assets = manifest.assets
  .filter((a) => a.platform === "all")
  .map((a) => ({ ...a, url: prefix + a.path }));
const wasmRoot = new URL(
  "../node_modules/onnxruntime-web/dist/",
  import.meta.url,
);
for (const name of [
  "ort-wasm-simd-threaded.wasm",
  "ort-wasm-simd-threaded.mjs",
]) {
  const bytes = await readFile(new URL(name, wasmRoot));
  assets.push({
    path: name,
    url: "/models/runtime/" + name,
    size: bytes.length,
    sha256: createHash("sha256").update(bytes).digest("hex"),
    platform: "web",
  });
  await mkdir(new URL("runtime/", publicRoot), { recursive: true });
  await writeFile(new URL("runtime/" + name, publicRoot), bytes);
}
if (source || download) {
  for (const a of manifest.assets.filter((a) => a.platform === "all")) {
    let bytes;
    if (source) bytes = await readFile(source + "/" + a.path);
    else {
      console.log("Downloading " + a.path);
      const response = await fetch(a.url, {
        signal: AbortSignal.timeout(300000),
      });
      if (!response.ok)
        throw Error(
          "Model download failed: " + a.path + " (HTTP " + response.status + ")",
        );
      bytes = Buffer.from(await response.arrayBuffer());
    }
    if (
      bytes.length !== a.size ||
      createHash("sha256").update(bytes).digest("hex") !== a.sha256
    )
      throw Error("Model asset verification failed: " + a.path);
    const target = new URL("bge-small/" + a.path, publicRoot);
    await mkdir(new URL(".", target), { recursive: true });
    await writeFile(target, bytes);
  }
}
const parts =
  `${manifest.revision}:${manifest.window}:${manifest.overlap}:${manifest.pooling}:${manifest.queryInstruction}:labels-v1` +
  manifest.assets
    .filter((a) => a.path === "onnx/model.onnx" || a.path === "tokenizer.json")
    .map((a) => ":" + a.sha256)
    .join("");
await mkdir(new URL("../src/", import.meta.url), { recursive: true });
await writeFile(
  new URL("../src/model-manifest.json", import.meta.url),
  JSON.stringify(
    {
      ...manifest,
      assets,
      fingerprint: createHash("sha256").update(parts).digest("hex"),
    },
    null,
    2,
  ) + "\n",
);
