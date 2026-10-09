import { useEffect, useState, useRef } from "react";
import {
  api,
  categories,
  errorMessage,
  type Analysis,
  type Item,
  type State,
  type SemanticStatus,
} from "./api";
export function SemanticSettings({
  state,
  onRefresh,
}: {
  state: State;
  onRefresh: () => Promise<void>;
}) {
  const [status, setStatus] = useState<SemanticStatus | null>(null),
    [error, setError] = useState(""),
    [op, setOp] = useState(""),
    [cutoff, setCutoff] = useState(0.5),
    [margin, setMargin] = useState(0.01);
  useEffect(() => {
    let active = true;
    void api()
      .SemanticStatus?.()
      .then((s) => {
        if (active) {
          setStatus(s);
          setCutoff(s.thresholds.cutoff);
          setMargin(s.thresholds.margin);
        }
      })
      .catch((e) => {
        if (active) setError(errorMessage(e));
      });
    return () => {
      active = false;
    };
  }, [state.semantic?.enabled, state.pending]);
  async function setup(local = false) {
    setError("");
    try {
      const id = await (local
        ? api().ImportSemanticModel?.()
        : api().SetupSemantic?.());
      if (id) {
        if (completed.current.has(id)) {
          const code = completed.current.get(id);
          if (code) setError(errorMessage(code));
          await onRefresh();
        } else setOp(id);
      }
    } catch (e) {
      setError(errorMessage(e));
    }
  }
  const completed = useRef(new Map<string, string>());
  useEffect(() => {
    if (!api().SemanticStatus) return;
    const off = window.runtime?.EventsOn("vault:event", (e) => {
      if (e.generation !== state.generation || !e.operation) return;
      if (e.kind === "finished") {
        completed.current.set(e.operation, e.error || "");
        if (completed.current.size > 64)
          completed.current.delete(completed.current.keys().next().value!);
      }
      if (e.operation !== op) return;
      if (e.kind === "finished") {
        setOp("");
        if (e.error) setError(errorMessage(e.error));
        void onRefresh();
      }
    });
    return () => off?.();
  }, [op, state.generation, onRefresh]);
  async function configure(enabled: boolean) {
    try {
      await api().ConfigureSemantic?.({
        enabled,
        thresholds: { cutoff, margin },
      });
      await onRefresh();
      setStatus((s) =>
        s ? { ...s, enabled, thresholds: { cutoff, margin } } : s,
      );
    } catch (e) {
      setError(errorMessage(e));
    }
  }
  if (!api().SemanticStatus) return null;
  return (
    <section className="settings-section">
      <h3>Semantic search and suggestions</h3>
      <p className="muted small">
        Optional, on this device. Find documents by meaning and review category,
        date, and amount suggestions. Processing stays local.
      </p>
      <p className="muted small">Model: BGE-small-en-v1.5.</p>
      {status && (
        <>
          <p className="muted small">
            {status.ready
              ? "Local model installed."
              : `One-time download: about ${Math.ceil(status.downloadBytes / 1048576)} MB plus runtime packaging. Works offline afterward.`}
          </p>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={status.enabled}
              disabled={!status.supported || Boolean(op)}
              onChange={(e) => {
                if (e.target.checked && !status.ready) void setup();
                else void configure(e.target.checked);
              }}
            />{" "}
            Enable on this device
          </label>
          {!status.supported && (
            <p className="muted small">
              This build does not include native ONNX support.
            </p>
          )}
          {status.supported && (
            <>
            {status.enabled && !status.ready && <button type="button" className="secondary" disabled={Boolean(op)} onClick={() => void setup()}>Install BGE-small model</button>}
            <button
              type="button"
              className="quiet"
              disabled={Boolean(op)}
              onClick={() => void setup(true)}
            >
              Select local model bundle
            </button>
            </>
          )}
          {status.enabled && (
            <details>
              <summary>Classification thresholds</summary>
              <label>
                Similarity cutoff
                <input
                  type="number"
                  min={-1}
                  max={1}
                  step={0.01}
                  value={cutoff}
                  onChange={(e) => setCutoff(Number(e.target.value))}
                />
              </label>
              <label>
                Winning category margin
                <input
                  type="number"
                  min={0}
                  max={2}
                  step={0.01}
                  value={margin}
                  onChange={(e) => setMargin(Number(e.target.value))}
                />
              </label>
              <button
                type="button"
                className="quiet"
                onClick={() => void configure(true)}
              >
                Save thresholds
              </button>
            </details>
          )}
        </>
      )}
      {op && (
        <p role="status">
          Setting up and indexing locally…{" "}
          <button
            type="button"
            className="quiet"
            onClick={() => void api().Cancel(op)}
          >
            Cancel
          </button>
        </p>
      )}
      {error && (
        <p role="alert" className="form-error">
          {error}
        </p>
      )}
    </section>
  );
}
export function AnalysisPanel({ item }: { item: Item }) {
  const [analysis, setAnalysis] = useState<Analysis | null>(null),
    [category, setCategory] = useState(""),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  useEffect(() => {
    let active = true;
    void api()
      .Analyze?.(item.revision)
      .then((a) => {
        if (active) {
          setAnalysis(a);
          setCategory(a.confirmed || a.prediction.category);
        }
      })
      .catch((e) => {
        if (active) setError(errorMessage(e));
      });
    return () => {
      active = false;
    };
  }, [item.revision]);
  async function save(resolve: string[] = []) {
    if (!analysis) return;
    setBusy(true);
    try {
      await api().CorrectAnalysis?.(
        item.revision,
        category,
        analysis.fields,
        resolve,
      );
      setAnalysis({ ...analysis, confirmed: category, conflicts: [] });
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="settings-section">
      <h3>Local suggestions</h3>
      {analysis && (
        <>
          <p className="muted small">
            {analysis.confirmed
              ? "Your confirmed category."
              : analysis.prediction.category
                ? `Suggested category: ${analysis.prediction.category}`
                : "No confident category suggestion."}
          </p>
          <label>
            Category
            <select
              value={category}
              onChange={(e) => setCategory(e.target.value)}
            >
              <option value="">Uncategorized</option>
              {categories.map((c) => (
                <option key={c}>{c}</option>
              ))}
            </select>
          </label>
          {analysis.fields.map((f, i) => (
            <label key={i}>
              {f.kind === "date" ? "Date" : "Amount"}
              {f.currency ? ` (${f.currency})` : ""}
              <input
                value={f.value || f.raw}
                onChange={(e) =>
                  setAnalysis({
                    ...analysis,
                    fields: analysis.fields.map((x, j) =>
                      j === i ? { ...x, value: e.target.value } : x,
                    ),
                  })
                }
              />
              <small>{f.evidence}</small>
            </label>
          ))}
          {analysis.conflicts.length > 1 ? (
            <>
              <p className="muted small">
                Different devices saved competing corrections. Select one,
                review its fields, then confirm your choice.
              </p>
              {analysis.conflicts.map((c) => (
                <button
                  type="button"
                  className="quiet"
                  key={c.revision}
                  onClick={() => {
                    setCategory(c.correction.category);
                    setAnalysis({ ...analysis, fields: c.correction.fields });
                  }}
                >
                  Use {c.correction.category || "uncategorized"} ·{" "}
                  {c.revision.slice(0, 6)}
                </button>
              ))}
              <button
                type="button"
                className="secondary"
                disabled={busy}
                onClick={() =>
                  void save(analysis.conflicts.map((c) => c.revision))
                }
              >
                Resolve with reviewed values
              </button>
            </>
          ) : (
            <button
              type="button"
              className="secondary"
              disabled={busy}
              onClick={() => void save()}
            >
              Confirm category and fields
            </button>
          )}
        </>
      )}
      {error && (
        <p role="alert" className="form-error">
          {error}
        </p>
      )}
    </section>
  );
}
