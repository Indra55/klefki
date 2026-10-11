import { useEffect, useRef, useState } from "react";
import { api, errorMessage, type OrganizationPlan, type TagSuggestion } from "./api";
import { Modal } from "./ui";

type Choice = TagSuggestion & { selected: boolean; group: string };

export function Organize({ generation, onClose, onApplied }: {
  generation: number;
  onClose: () => void;
  onApplied: (count: number) => Promise<void>;
}) {
  const [custom, setCustom] = useState("");
  const [plan, setPlan] = useState<OrganizationPlan | null>(null);
  const [choices, setChoices] = useState<Choice[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const active = useRef(true);
  useEffect(() => { active.current = true; return () => { active.current = false; }; }, []);

  async function suggest() {
    setBusy(true);
    setError("");
    setPlan(null);
    setChoices([]);
    try {
      const result = await api().SuggestOrganization!(Array.from(new Set(custom.split(",").map(t => t.trim()).filter(Boolean))));
      if (!active.current) return;
      if (result.generation !== generation) throw new Error("STALE");
      setPlan(result);
      setChoices(result.suggestions.map(s => ({ ...s, selected: true, group: s.tag })));
    } catch (e) {
      if (active.current) setError(errorMessage(e));
    } finally {
      if (active.current) setBusy(false);
    }
  }

  async function apply() {
    setBusy(true);
    setError("");
    try {
      const count = await api().ApplyOrganization!(generation, choices.filter(c => c.selected).map(c => ({ revision: c.revision, tag: c.tag.trim() })));
      if (active.current) await onApplied(count);
    } catch (e) {
      if (active.current) setError(errorMessage(e));
    } finally {
      if (active.current) setBusy(false);
    }
  }

  const selected = choices.filter(c => c.selected);
  const groups = Array.from(new Set(choices.map(c => c.group)));
  return <Modal title="Organize files" description="Find tags by meaning, then review before saving. Existing tags and folders are kept. Processing stays on this device." onClose={busy ? () => {} : onClose}>
    <label>Custom tags to match
      <input aria-label="Custom tags to match" value={custom} onChange={e => setCustom(e.target.value)} placeholder="Travel, Recipes, Research" disabled={busy} />
      <small>Separate tags with commas. Existing tags are included automatically.</small>
    </label>
    <button className="secondary organize-suggest" disabled={busy} onClick={() => void suggest()}>{busy && !plan ? "Finding tags locally…" : plan ? "Find tags again" : "Find tags"}</button>
    {plan && <>
      <p className="muted small">{choices.length ? `${choices.length} files to review.` : "No new tags to apply."} {plan.skipped > 0 && `${plan.skipped} files skipped: already tagged, unresolved conflicts, tag limits, or no indexed text.`}</p>
      {choices.length > 0 && <button className="quiet" disabled={busy} onClick={() => setChoices(rows => rows.map(r => ({ ...r, selected: selected.length !== choices.length })))}>{selected.length === choices.length ? "Deselect all" : "Select all"}</button>}
      <datalist id="organization-tags">{plan.tags.map(t => <option key={t} value={t} />)}</datalist>
      {groups.map(group => {
        const rows = choices.filter(c => c.group === group);
        const first = rows[0];
        return <section className="organize-group" key={group}>
          <label>{first.new ? "New tag" : "Matching tag"}
            <input aria-label={`Tag for ${group}`} list="organization-tags" maxLength={80} value={first.tag} disabled={busy} onChange={e => setChoices(old => old.map(c => c.group === group ? { ...c, tag: e.target.value } : c))} />
          </label>
          <p className="muted small">{first.basis === "confirmed category" ? "From your confirmed category." : first.new ? "Grouped by similar content. Review or rename this suggested tag." : "Matched by meaning or files you have already tagged."}</p>
          {rows.map(row => <label className="checkbox organize-file" key={row.revision}>
            <input type="checkbox" checked={row.selected} disabled={busy} onChange={e => setChoices(old => old.map(c => c.revision === row.revision ? { ...c, selected: e.target.checked } : c))} />
            <span>{row.name}{row.tags.length > 0 && <small> Keeping: {row.tags.join(", ")}</small>}</span>
          </label>)}
        </section>;
      })}
    </>}
    {error && <p role="alert" className="form-error">{error}</p>}
    {busy && <p role="status" className="muted small">{plan ? "Saving reviewed tags…" : "Comparing document embeddings…"}</p>}
    <div className="form-actions">
      <button className="quiet" disabled={busy} onClick={onClose}>Cancel</button>
      {plan && choices.length > 0 && <button className="primary" disabled={busy || !selected.length || selected.some(c => !c.tag.trim())} onClick={() => void apply()}>Apply tags to {selected.length} {selected.length === 1 ? "file" : "files"}</button>}
    </div>
  </Modal>;
}
