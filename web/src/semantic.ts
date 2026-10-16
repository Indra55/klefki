import * as c from "./crypto.ts";
import { Store, type Head } from "./store.ts";
import manifest from "./model-manifest.json";
import { relevantQueryTerms, keywordEvidence, relevantSemanticHits } from "./search-ranking.ts";
export const semanticTag = "klefki:system:semantic-index",
  correctionTag = "klefki:system:analysis-correction",
  indexTag = "klefki:system:search-index";
import { categories } from '../../ui/categories.ts';
export { categories };
export type Field = {
  kind: "date" | "amount";
  raw: string;
  value: string;
  currency?: string;
  evidence: string;
};
export type Correction = {
  version: 1;
  object: string;
  fileID: string;
  category: string;
  fields: Field[];
  supersedes: string[];
};
export type CorrectionRecord = { revision: string; correction: Correction };
type Doc = {
  object: string;
  revision: string;
  fileID: string;
  name: number[];
  chunks: number[][];
  vector: number[] | null;
  fields: Field[];
};
type Index = {
  version: number;
  fingerprint: string;
  extraction: string;
  labels: number[][];
  documents: Doc[];
};
export type Preferences = { enabled: boolean; cutoff: number; margin: number; modelFingerprint?: string };
export const defaults: Preferences = { enabled: false, cutoff: 0.5, margin: 0.01 };
export const cacheName = "klefki-model-" + manifest.fingerprint;
export const downloadBytes = manifest.assets.reduce((n, a) => n + a.size, 0);
const installedURL = "/models/setup-complete";
const installedAssets = JSON.stringify(manifest.assets.map(a => a.sha256));
export async function modelInstalled() {
  if (!await caches.has(cacheName)) return false;
  const cache = await caches.open(cacheName);
  const marker = await cache.match(installedURL);
  if (!marker || await marker.text() !== installedAssets) return false;
  const urls = new Set((await cache.keys()).map(request => request.url));
  return manifest.assets.every(a => urls.has(new URL(a.url, location.origin).href));
}
export function preferences(account: string): Preferences {
  try {
    const p = JSON.parse(
      localStorage.getItem("klefki-semantic-" + account) || "null",
    );
    if (
      p &&
      typeof p.enabled === "boolean" &&
      Number.isFinite(p.cutoff) &&
      Number.isFinite(p.margin) &&
      p.cutoff >= -1 &&
      p.cutoff <= 1 &&
      p.margin >= 0 &&
      p.margin <= 2
    )
      return p.modelFingerprint === manifest.fingerprint ? p : { ...defaults, enabled: p.enabled };
  } catch {}
  return { ...defaults };
}
export function savePreferences(account: string, p: Preferences) {
  localStorage.setItem("klefki-semantic-" + account, JSON.stringify({ ...p, modelFingerprint: manifest.fingerprint }));
}
export function internal(h: Head) {
  return h.metadata.tags.some((t) =>
    [semanticTag, correctionTag, indexTag].includes(t),
  );
}
function check(ok: unknown): asserts ok {
  if (!ok) throw Error("SEARCH_INDEX_INVALID");
}
export function vector(v: unknown): v is number[] {
  if (
    !Array.isArray(v) ||
    v.length !== 384 ||
    v.some((x) => typeof x !== "number" || !Number.isFinite(x))
  )
    return false;
  return Math.abs(v.reduce((n, x) => n + x * x, 0) - 1) < 0.002;
}
function cosine(a: number[], b: number[]) {
  return Math.max(
    -1,
    Math.min(
      1,
      a.reduce((n, x, i) => n + x * b[i], 0),
    ),
  );
}
function fields(fs: unknown): fs is Field[] {
  return (
    Array.isArray(fs) &&
    fs.length <= 256 &&
    fs.every(
      (f) =>
        f &&
        (f.kind === "date" || f.kind === "amount") &&
        typeof f.raw === "string" &&
        f.raw.length > 0 &&
        typeof f.value === "string" &&
        typeof f.evidence === "string" &&
        f.evidence.length > 0 &&
        [f.raw, f.value, f.evidence, f.currency ?? ""].every(
          (x) =>
            typeof x === "string" &&
            new TextEncoder().encode(x).length <= 512 &&
            !x.includes("\0"),
        ) &&
        (f.currency === undefined ||
          ["INR", "USD", "EUR", "GBP"].includes(f.currency)) &&
        (f.kind !== "date" || !f.currency),
    )
  );
}
function only(x: object, names: string[]) {
  check(Object.keys(x).every((k) => names.includes(k)));
}
async function payload(store: Store, key: Uint8Array, h: Head) {
  check(h.metadata.type === "file" && h.metadata.size! <= 32 * 1048576);
  const bytes = await store.export(key, h.revision);
  try {
    return JSON.parse(c.decode(bytes));
  } catch {
    throw Error("SEARCH_INDEX_INVALID");
  } finally {
    c.wipe(bytes);
  }
}
function visible(heads: Head[]) {
  function hidden(h: Head, seen = new Set<string>()): boolean {
    if (
      internal(h) ||
      h.metadata.tags.includes("klefki:system:trash") ||
      seen.has(h.revision.object)
    )
      return true;
    if (!h.metadata.parent) return false;
    seen.add(h.revision.object);
    return heads
      .filter((p) => p.revision.object === h.metadata.parent)
      .some((p) => hidden(p, new Set(seen)));
  }
  return heads
    .filter((h) => h.metadata.type === "file" && !hidden(h))
    .sort((a, b) => a.revision.id.localeCompare(b.revision.id));
}
async function index(
  store: Store,
  key: Uint8Array,
  heads: Head[],
): Promise<Index> {
  const docs = visible(heads);
  for (const h of heads.filter((h) => h.metadata.tags.includes(semanticTag))) {
    const x = await payload(store, key, h);
    only(x, ["version", "fingerprint", "extraction", "labels", "documents"]);
    check(
      x.version === 1 &&
        typeof x.fingerprint === "string" &&
        x.fingerprint.length <= 128 &&
        x.extraction === "klefki:system:search-image-cache-v1" &&
        Array.isArray(x.labels) &&
        x.labels.length === 6 &&
        x.labels.every(vector) &&
        Array.isArray(x.documents),
    );
    for (const d of x.documents) {
      only(d, [
        "object",
        "revision",
        "fileID",
        "name",
        "chunks",
        "vector",
        "fields",
      ]);
      c.validID(d.object);
      c.validID(d.revision);
      c.validID(d.fileID);
      check(
        vector(d.name) &&
          Array.isArray(d.chunks) &&
          d.chunks.every(vector) &&
          (d.chunks.length ? vector(d.vector) : d.vector === null) &&
          fields(d.fields),
      );
    }
    if (
      x.fingerprint !== manifest.fingerprint ||
      x.documents.length !== docs.length
    )
      continue;
    if (
      x.documents.every(
        (d: Doc, i: number) =>
          d.object === docs[i].revision.object &&
          d.revision === docs[i].revision.id &&
          d.fileID === docs[i].metadata.file_id,
      )
    )
      return x;
  }
  throw Error("SEARCH_REBUILD_REQUIRED");
}
export async function corrections(
  store: Store,
  key: Uint8Array,
  heads: Head[],
): Promise<CorrectionRecord[]> {
  const records: CorrectionRecord[] = [];
  for (const h of heads.filter((h) =>
    h.metadata.tags.includes(correctionTag),
  )) {
    const x = await payload(store, key, h);
    only(x, [
      "version",
      "object",
      "fileID",
      "category",
      "fields",
      "supersedes",
    ]);
    c.validID(x.object);
    c.validID(x.fileID);
    check(
      x.version === 1 &&
        typeof x.category === "string" &&
        (!x.category || categories.includes(x.category)) &&
        fields(x.fields) &&
        Array.isArray(x.supersedes) &&
        x.supersedes.length <= 64,
    );
    x.supersedes.forEach(c.validID);
    records.push({ revision: h.revision.id, correction: x });
  }
  const superseded = new Set<string>();
  for (const r of records)
    for (const id of r.correction.supersedes) {
      const other = records.find((x) => x.revision === id);
      check(
        other &&
          id !== r.revision &&
          other.correction.object === r.correction.object &&
          other.correction.fileID === r.correction.fileID,
      );
      superseded.add(id);
    }
  return records
    .filter((r) => !superseded.has(r.revision))
    .sort((a, b) => a.revision.localeCompare(b.revision));
}
export async function correct(
  store: Store,
  key: Uint8Array,
  heads: Head[],
  source: Head,
  category: string,
  fs: Field[],
  resolve: string[] = [],
) {
  check(
    !internal(source) &&
      visible(heads).some((h) => h.revision.id === source.revision.id),
  );
  check((!category || categories.includes(category)) && fields(fs));
  const existing = (await corrections(store, key, heads))
    .filter(
      (r) =>
        r.correction.object === source.revision.object &&
        r.correction.fileID === source.metadata.file_id,
    )
    .map((r) => r.revision)
    .sort();
  if (
    existing.length > 1 &&
    (resolve.length !== existing.length ||
      resolve
        .slice()
        .sort()
        .some((id, i) => id !== existing[i]))
  )
    throw Error("SEMANTIC_CORRECTION_CONFLICT");
  const value: Correction = {
    version: 1,
    object: source.revision.object,
    fileID: source.metadata.file_id!,
    category,
    fields: fs,
    supersedes: existing,
  };
  const bytes = c.utf8(JSON.stringify(value));
  try {
    return await store.put(
      key,
      {
        type: "file",
        name: "analysis-correction.v1.json",
        parent: null,
        tags: [correctionTag],
        favorite: false,
      },
      bytes,
    );
  } finally {
    c.wipe(bytes);
  }
}
export async function analyze(
  store: Store,
  key: Uint8Array,
  heads: Head[],
  source: Head,
  p: Preferences,
) {
  const ix = await index(store, key, heads),
    d = ix.documents.find((d) => d.revision === source.revision.id);
  if (!d) throw Error("SEARCH_REBUILD_REQUIRED");
  const scores = ix.labels
    .map((v, i) => ({
      category: categories[i],
      score: d.vector ? cosine(d.vector, v) : -1,
    }))
    .sort((a, b) => b.score - a.score);
  let category =
    d.vector &&
    scores[0].score >= p.cutoff &&
    scores[0].score - scores[1].score >= p.margin
      ? scores[0].category
      : "";
  let method = "zero-shot";
  const records = await corrections(store, key, heads),
    own = records.filter(
      (r) =>
        r.correction.object === d.object && r.correction.fileID === d.fileID,
    );
  const neighbors = ix.documents
    .filter((x) => x.object !== d.object && x.vector && d.vector)
    .flatMap((x) => {
      const matches = records.filter(
        (r) =>
          r.correction.object === x.object &&
          r.correction.fileID === x.fileID &&
          r.correction.category,
      );
      const score = cosine(d.vector!, x.vector!);
      return matches.length === 1 && score >= 0.5
        ? [{ category: matches[0].correction.category, score, id: x.revision }]
        : [];
    })
    .sort((a, b) => b.score - a.score || a.id.localeCompare(b.id))
    .slice(0, 5);
  if (neighbors.length >= 3) {
    const total = neighbors.reduce((n, x) => n + x.score, 0);
    for (const label of categories)
      if (
        neighbors
          .filter((n) => n.category === label)
          .reduce((n, x) => n + x.score, 0) /
          total >=
        0.6
      ) {
        category = label;
        method = "knn";
        break;
      }
  }
  return {
    category: own.length === 1 ? own[0].correction.category : category,
    confirmed: own.length === 1,
    method,
    fields: own.length === 1 ? own[0].correction.fields : d.fields,
    conflicts: own.length > 1 ? own : [],
  };
}
let worker: Worker | undefined,
  nextID = 0;
const pending = new Map<
  number,
  {
    resolve: (x: {
      tokens: number[];
      vector: number[];
      fingerprint: string;
    }) => void;
    reject: (e: Error) => void;
  }
>();
export function clearPrivate() {
  worker?.terminate();
  worker = undefined;
  for (const p of pending.values()) p.reject(Error("LOCKED"));
  pending.clear();
}
export function embed(text: string) {
  if (!worker) {
    worker = new Worker(new URL("./semantic-worker.ts", import.meta.url), {
      type: "module",
    });
    worker.onmessage = (e) => {
      const p = pending.get(e.data.id);
      pending.delete(e.data.id);
      if (!p) return;
      if (e.data.error) p.reject(Error(e.data.error));
      else p.resolve(e.data.result);
    };
    worker.onerror = () => {
      for (const p of pending.values()) p.reject(Error("SEMANTIC_UNAVAILABLE"));
      pending.clear();
      worker?.terminate();
      worker = undefined;
    };
  }
  return new Promise<{
    tokens: number[];
    vector: number[];
    fingerprint: string;
  }>((resolve, reject) => {
    const id = ++nextID;
    pending.set(id, { resolve, reject });
    worker!.postMessage({ id, text });
  });
}
export async function setup(
  signal: AbortSignal,
  onProgress: (done: number, total: number) => void,
  bundle?: File[],
) {
  const cache = await caches.open(cacheName);
  let done = 0;
  for (const a of manifest.assets) {
    if (signal.aborted) throw Error("CANCELED");
    const cached = await cache.match(a.url);
    let bytes: Uint8Array;
    if (cached) bytes = new Uint8Array(await cached.arrayBuffer());
    else if (bundle && a.platform !== "web") {
      const file = bundle.find(
        (f) => f.webkitRelativePath.endsWith("/" + a.path) || f.name === a.path,
      );
      if (!file) throw Error("SEMANTIC_MODEL_INVALID");
      bytes = new Uint8Array(await file.arrayBuffer());
    } else {
      const r = await fetch(a.url, { signal, credentials: "omit" });
      if (!r.ok) throw Error("SEMANTIC_UNAVAILABLE");
      const reader = r.body!.getReader(),
        chunks: Uint8Array[] = [];
      let size = 0;
      while (true) {
        const part = await reader.read();
        if (part.done) break;
        size += part.value.length;
        if (size > a.size) {
          await reader.cancel();
          throw Error("SEMANTIC_MODEL_INVALID");
        }
        chunks.push(part.value);
        onProgress(done + size, downloadBytes);
      }
      bytes = new Uint8Array(size);
      let offset = 0;
      for (const part of chunks) {
        bytes.set(part, offset);
        offset += part.length;
      }
    }
    const hash = Array.from(
      new Uint8Array(
        await crypto.subtle.digest("SHA-256", bytes as Uint8Array<ArrayBuffer>),
      ),
      (v) => v.toString(16).padStart(2, "0"),
    ).join("");
    if (bytes.length !== a.size || hash !== a.sha256)
      throw Error("SEMANTIC_MODEL_INVALID");
    if (signal.aborted) throw Error("CANCELED");
    const type = a.path.endsWith(".mjs")
      ? "text/javascript"
      : a.path.endsWith(".wasm")
        ? "application/wasm"
        : a.path.endsWith(".json")
          ? "application/json"
          : "application/octet-stream";
    await cache.put(
      a.url,
      new Response(bytes as Uint8Array<ArrayBuffer>, {
        headers: { "Content-Type": type },
      }),
    );
    done += a.size;
    onProgress(done, downloadBytes);
  }
  try {
    await embed("Local model setup verification.");
  } catch (error) {
    clearPrivate();
    throw error;
  }
  if (signal.aborted) throw Error("CANCELED");
  await cache.put(installedURL, new Response(installedAssets));
}
type Hit = { revision: string; object: string; name: string; score: number };
function rank(hits: Hit[]) {
  return hits.sort(
    (a, b) => b.score - a.score || a.revision.localeCompare(b.revision),
  );
}
function tokens(text: string) {
  return text
    .toLowerCase()
    .split(/[^\p{L}\p{N}\p{M}]+/u)
    .filter((t) => t && new TextEncoder().encode(t).length <= 128);
}
async function keyword(
  store: Store,
  key: Uint8Array,
  heads: Head[],
  query: string,
): Promise<Hit[]> {
  const docs = visible(heads);
  for (const h of heads.filter(
    (h) =>
      h.metadata.tags.includes(indexTag) &&
      h.metadata.tags.includes("klefki:system:search-image-cache-v1"),
  )) {
    const x = await payload(store, key, h);
    check(x.version === 1 && Array.isArray(x.documents));
    if (
      x.documents.length !== docs.length ||
      !x.documents.every(
        (d: any, i: number) =>
          d.object === docs[i].revision.object &&
          d.revision === docs[i].revision.id,
      )
    )
      continue;
    const allTerms = [...new Set(tokens(query))];
    if (allTerms.length > 64) throw Error("SEARCH_LIMIT");
    const terms = relevantQueryTerms(allTerms);
    const avg =
      x.documents.reduce((n: number, d: any) => n + d.length, 0) / docs.length;
    if (!avg) return [];
    const df = new Map(
      terms.map((t) => [
        t,
        x.documents.filter((d: any) => d.terms[t] > 0).length,
      ]),
    );
    const hits: Hit[] = [];
    for (let i = 0; i < x.documents.length; i++) {
      const d = x.documents[i];
      check(
        d.terms &&
          typeof d.terms === "object" &&
          Number.isSafeInteger(d.length) &&
          d.length >= 0,
      );
      let length = 0;
      for (const [term, n] of Object.entries(d.terms)) {
        check(
          Number.isSafeInteger(n) &&
            (n as number) > 0 &&
            (n as number) <= 64 * 1048576 &&
            tokens(term).length === 1 &&
            tokens(term)[0] === term,
        );
        length += n as number;
      }
      check(length === d.length);
      let score = 0;
      for (const t of terms) {
        const tf = d.terms[t] ?? 0;
        if (tf)
          score +=
            (Math.log1p((docs.length - df.get(t)! + 0.5) / (df.get(t)! + 0.5)) *
              tf *
              2.2) /
            (tf + 1.2 * (0.25 + (0.75 * d.length) / avg));
      }
      if (score > 0 && keywordEvidence(terms, d.terms))
        hits.push({
          revision: d.revision,
          object: d.object,
          name: docs[i].metadata.name,
          score,
        });
    }
    return rank(hits);
  }
  throw Error("SEARCH_REBUILD_REQUIRED");
}
export async function search(
  store: Store,
  key: Uint8Array,
  heads: Head[],
  query: string,
) {
  if (new TextEncoder().encode(query).length > 4096)
    throw Error("SEARCH_LIMIT");
  if (!query.trim()) return { hits: [], mode: "hybrid", availability: "ready" };
  let kw: Hit[] = [],
    kwReady = true;
  try {
    kw = await keyword(store, key, heads, query);
  } catch {
    kwReady = false;
  }
  try {
    const ix = await index(store, key, heads),
      q = await embed(manifest.queryInstruction + query);
    check(q.fingerprint === ix.fingerprint && vector(q.vector));
    const docs = visible(heads);
    const shortQuery = relevantQueryTerms(tokens(query)).length <= 3;
    const semanticHits = relevantSemanticHits(rank(
      ix.documents.flatMap((d, i) => {
        const score = Math.max(
          shortQuery ? cosine(q.vector, d.name) : -1,
          ...d.chunks.map((v) => cosine(q.vector, v)),
        );
        return score >= manifest.searchCutoff
          ? [
              {
                revision: d.revision,
                object: d.object,
                name: docs[i].metadata.name,
                score,
              },
            ]
          : [];
      }),
    ));
    const merged = new Map<string, Hit>();
    for (const list of [kw, semanticHits])
      for (const [i, h] of list.slice(0, 50).entries()) {
        const old = merged.get(h.revision) ?? { ...h, score: 0 };
        old.score += 1 / (61 + i);
        merged.set(h.revision, old);
      }
    return {
      hits: rank([...merged.values()]).slice(0, 20),
      mode: kwReady ? "hybrid" : "semantic",
      availability: kwReady ? "ready" : "KEYWORD_INDEX_UNAVAILABLE",
    };
  } catch (e) {
    return {
      hits: kw,
      mode: "keyword",
      availability: e instanceof Error ? e.message : "SEMANTIC_UNAVAILABLE",
    };
  }
}
