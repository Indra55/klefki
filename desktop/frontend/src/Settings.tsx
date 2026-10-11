import { useEffect, useRef, useState } from 'react';
import { ArrowLeft, ArrowUpRight, Cloud, LockKeyhole, Palette, Search } from 'lucide-react';
import { api, errorMessage, type Preferences, type State } from './api';
import { FingerprintIcon, type FingerprintAnimation } from './FingerprintIcon';
import { SemanticSettings } from './Semantic';

export type SettingsSection = 'appearance' | 'privacy' | 'sync' | 'search';
const sections = [
  { id: 'appearance', label: 'Appearance', icon: Palette },
  { id: 'privacy', label: 'Privacy & lock', icon: LockKeyhole },
  { id: 'sync', label: 'Sync & devices', icon: Cloud },
  { id: 'search', label: 'Search', icon: Search },
] as const;

export function Settings({ state, section, onSectionChange, scan, onScanStart, onClose, onRefresh, onRebuildIndex, indexing, operationError }: {
  state: State;
  section: SettingsSection;
  onSectionChange: (section: SettingsSection) => void;
  scan: string;
  onScanStart: () => void;
  onClose: () => void;
  onRefresh: () => Promise<void>;
  onRebuildIndex: () => void;
  indexing: boolean;
  operationError: string;
}) {
  const [url, setURL] = useState(state.config.server);
  const [syncMode,setSyncMode]=useState(state.syncMode || 'persistent');
  const [register, setRegister] = useState(false);
  const [busy, setBusy] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  const [fingerprintAction, setFingerprintAction] = useState<'enable' | 'disable' | null>(null);
  const [fingerprintResult, setFingerprintResult] = useState<FingerprintAnimation>('idle');
  const heading = useRef<HTMLHeadingElement>(null);
  const preferences = state.config.preferences;
  const fingerprintBusy = fingerprintAction === 'enable';

  useEffect(() => { heading.current?.focus(); }, []);
  useEffect(()=>{setSyncMode(state.syncMode || 'persistent');},[state.syncMode]);
  useEffect(() => { setError(''); setMessage(''); }, [section]);

  async function change(p: Preferences) {
    setSaving(true);
    setError('');
    setMessage('');
    try {
      await api().SetPreferences(p);
      await onRefresh();
      setMessage('Preferences saved on this device.');
    } catch (e) { setError(errorMessage(e)); }
    finally { setSaving(false); }
  }

  async function changeFingerprint() {
    const enabling = !state.fingerprint?.enabled;
    setError('');
    setMessage('');
    setFingerprintResult('idle');
    setFingerprintAction(enabling ? 'enable' : 'disable');
    if (enabling) onScanStart();
    try {
      if (enabling) {
        await api().EnableFingerprint();
        setFingerprintResult('success');
        setMessage('Fingerprint unlock enabled on this device.');
      } else {
        await api().DisableFingerprint();
        setMessage('Fingerprint access removed from this device.');
      }
      await api().FingerprintStatus();
      await onRefresh();
    } catch (e) {
      if (enabling) setFingerprintResult(String(e instanceof Error ? e.message : e) === 'FINGERPRINT_CANCELED' ? 'idle' : 'error');
      setError(errorMessage(e));
    } finally { setFingerprintAction(null); }
  }

  return <main className="settings-page">
    <button className="quiet settings-back" onClick={onClose}><ArrowLeft size={16} /> Back to library</button>
    <header className="settings-heading">
      <h1 ref={heading} tabIndex={-1}>Settings</h1>
      <p>Make Klefki feel at home. Preferences are saved on this device.</p>
    </header>
    <div className="settings-layout">
      <nav className="settings-nav" aria-label="Settings sections">
        {sections.map(({ id, label, icon: Icon }) => <button key={id} disabled={saving || busy || fingerprintAction !== null} aria-current={section === id ? 'page' : undefined} onClick={() => onSectionChange(id)}><Icon size={17} /><span>{label}</span></button>)}
      </nav>
      <div className="settings-panel" aria-busy={saving}>
        {section === 'appearance' && <section aria-labelledby="appearance-heading">
          <header className="settings-panel-heading"><h2 id="appearance-heading">Appearance</h2><p>Choose how your library looks and how you browse it.</p></header>
          <div className="preference-row">
            <div><label htmlFor="theme">Color theme</label><p id="theme-help">Use a light or dark theme, or follow your device.</p></div>
            <select id="theme" aria-describedby="theme-help" value={preferences.theme} disabled={saving} onChange={e => void change({ ...preferences, theme: e.target.value as Preferences['theme'] })}><option value="dark">Dark</option><option value="light">Light</option><option value="system">Follow system</option></select>
          </div>
          <div className="preference-row">
            <div><label htmlFor="density">Document spacing</label><p id="density-help">Give documents room to breathe, or fit more rows on screen.</p></div>
            <select id="density" aria-describedby="density-help" value={preferences.density || 'comfortable'} disabled={saving} onChange={e => void change({ ...preferences, density: e.target.value as Preferences['density'] })}><option value="comfortable">Comfortable</option><option value="compact">Compact</option></select>
          </div>
          <div className="preference-row">
            <div><label htmlFor="default-sort">Default sort order</label><p id="sort-help">Start with this order when you open the library.</p></div>
            <select id="default-sort" aria-describedby="sort-help" value={preferences.defaultSort || 'name'} disabled={saving} onChange={e => void change({ ...preferences, defaultSort: e.target.value as Preferences['defaultSort'] })}><option value="name">Name</option><option value="type">Type</option><option value="size">Size, largest first</option></select>
          </div>
        </section>}

        {section === 'privacy' && <section aria-labelledby="privacy-heading">
          <header className="settings-panel-heading"><h2 id="privacy-heading">Privacy & lock</h2><p>Choose when to lock your library and how to unlock it.</p></header>
          <div className="preference-row">
            <div><label htmlFor="idle">Lock after inactivity</label><p id="idle-help">Klefki also locks when minimized.</p></div>
            <select id="idle" aria-describedby="idle-help" value={preferences.idleMinutes} disabled={saving} onChange={e => void change({ ...preferences, idleMinutes: Number(e.target.value) })}>{[1, 5, 15, 30].map(n => <option key={n} value={n}>{n === 1 ? '1 minute' : `${n} minutes`}</option>)}</select>
          </div>
          <p className="settings-hint">Lock immediately with <kbd>Ctrl</kbd> <kbd>Shift</kbd> <kbd>L</kbd>.</p>
          {state.fingerprint?.supported && <section className="settings-section">
            <h3>Fingerprint unlock</h3>
            <p className="muted small">Use the fingerprint enrolled in Linux to unlock this store on this device.</p>
            {!state.fingerprint.available && <p className="muted small">{errorMessage(state.fingerprint.reason || 'FINGERPRINT_UNAVAILABLE')}</p>}
            <button className="secondary fingerprint-button" disabled={busy || fingerprintAction !== null || (!state.fingerprint.enabled && (!state.fingerprint.available || state.recoveryOnly))} aria-busy={fingerprintAction !== null} onClick={() => void changeFingerprint()}>
              <FingerprintIcon status={fingerprintBusy && fingerprintResult !== 'success' ? (scan === 'retry' ? 'retry' : scan === 'scanning' ? 'scanning' : 'idle') : fingerprintResult} />
              <span>{state.fingerprint.enabled ? 'Disable fingerprint unlock' : 'Enable fingerprint unlock'}{fingerprintAction !== null && <span className="fingerprint-status" role="status">{fingerprintAction === 'disable' ? 'Removing fingerprint access…' : fingerprintResult === 'success' ? 'Fingerprint verified' : scan === 'retry' ? 'Lift your finger and try again…' : scan === 'scanning' ? 'Reading your fingerprint…' : 'Touch your fingerprint reader…'}</span>}</span>
            </button>
            {fingerprintBusy && <button className="quiet cancel-scan" onClick={() => void api().CancelFingerprint().catch(e => setError(errorMessage(e)))}>Cancel scan</button>}
            {state.recoveryOnly && !state.fingerprint.enabled && <p className="muted small">Unlock with your password to enable fingerprint access.</p>}
            <details className="settings-details"><summary>How fingerprint access is stored</summary><p>Klefki saves its unlock keys in your Linux keyring. Fingerprint checks are enforced by Klefki; other apps with access to the unlocked keyring may access those keys.</p></details>
          </section>}
        </section>}

        {section === 'sync' && <section aria-labelledby="sync-heading">
          <header className="settings-panel-heading"><h2 id="sync-heading">Sync & devices</h2><p>Keep the same encrypted library on your devices. You can still work offline.</p></header>
          <div className="connection-status"><div><span>{state.sync}</span><p>{state.pending ? `${state.pending} ${state.pending === 1 ? 'change waiting' : 'changes waiting'} to sync` : 'No local changes waiting to sync.'}</p>{state.lastSync && <p>Last synced {new Date(state.lastSync).toLocaleString()}</p>}</div>{state.config.server && !state.recoveryOnly && <button className="secondary" disabled={state.sync === 'Syncing' || busy} onClick={() => void api().SyncNow().catch(e => setError(errorMessage(e)))}>Sync now</button>}</div>
          <form className="settings-connect" onSubmit={async e => {
            e.preventDefault(); setBusy(true); setError(''); setMessage('');
            try { await api().Connect(url, register); setMessage('Connected. Your documents and folders will sync automatically.'); await onRefresh(); }
            catch (e) { setError(errorMessage(e)); }
            finally { setBusy(false); }
          }}>
            <label htmlFor="server-address">Server address</label>
            <input id="server-address" aria-describedby="server-help" type="url" placeholder="https://klefki.example.com" value={url} onChange={e => setURL(e.target.value)} required disabled={state.recoveryOnly || busy} />
            <p id="server-help" className="settings-hint">Use the same server on each device. Documents, folders, and their names are encrypted before syncing.</p>
            {!state.config.server && <label className="checkbox"><input type="checkbox" disabled={busy || state.recoveryOnly} checked={register} onChange={e => setRegister(e.target.checked)} /> Register this store on the server for the first time</label>}
            <button className="secondary" disabled={busy || state.recoveryOnly}>{busy ? 'Connecting…' : state.config.server ? 'Reconnect' : 'Connect server'}</button>
          </form>
          {state.recoveryOnly && <p className="muted small">Unlock with your password to sync. Recovery access is local.</p>}
          <section className="settings-section">
            <h3>Server storage</h3>
            <div className="preference-row"><div><label htmlFor="sync-mode">Sync mode</label><p>Shared by every device linked to this account.</p></div>
              <select id="sync-mode" value={syncMode} disabled={busy || state.recoveryOnly} onChange={e=>setSyncMode(e.target.value as typeof syncMode)}><option value="persistent">Persistent storage</option><option value="temporary-relay">Temporary relay</option></select>
            </div>
            <p className="muted small">{syncMode==='temporary-relay'?'The server removes delivered documents after every linked device has saved them, with at least two devices required. Offline devices keep deliveries queued. Keep a local backup. New devices need an existing device to reconnect.':'Keep encrypted documents on the server so devices can download them later.'}</p>
            {syncMode==='temporary-relay' && <p className="muted small">Free hosts can erase queued deliveries on restart. Existing devices retain their copies and supply them again when they reconnect.</p>}
            <button className="secondary" disabled={busy || state.recoveryOnly || !state.config.server || syncMode===(state.syncMode || 'persistent')} onClick={()=>void(async()=>{setBusy(true);setError('');setMessage('');try{await api().SetSyncMode(syncMode);await onRefresh();setMessage('Sync mode updated for this account.');}catch(e){setError(errorMessage(e));}finally{setBusy(false);}})()}>Apply sync mode</button>
          </section>
          <section className="settings-section">
            <h3>Set up another device</h3>
            <p className="muted small">Save your encrypted account file, open it on the other device, and use your password and the same server.</p>
            <button className="quiet" onClick={() => void api().ExportAccount().catch(e => setError(errorMessage(e)))}>Save encrypted account file <ArrowUpRight size={15} /></button>
            <details className="settings-details"><summary>Account and local storage</summary><dl><dt>Account ID</dt><dd>{state.accountID}</dd><dt>Account file</dt><dd>{state.config.accountPath}</dd><dt>Document store</dt><dd>{state.config.storePath}</dd></dl></details>
          </section>
          {!!state.syncDevices?.length && <section className="settings-section"><h3>Linked devices</h3><p className="muted small">Forget a device you no longer use so it stops holding deliveries in the relay. It will need a fresh local store to join again.</p>{state.syncDevices.map(id=><div className="preference-row" key={id}><span>{id===state.syncDevice?'This device':`Device ${id.slice(0,8)}`}</span>{id!==state.syncDevice&&<button className="quiet" disabled={busy||state.recoveryOnly} onClick={()=>void(async()=>{setBusy(true);setError('');try{await api().ForgetSyncDevice(id);await onRefresh();setMessage('Device removed from relay deliveries.');}catch(e){setError(errorMessage(e));}finally{setBusy(false);}})()}>Forget device</button>}</div>)}</section>}
        </section>}

        {section === 'search' && <section aria-labelledby="search-heading">
          <header className="settings-panel-heading"><h2 id="search-heading">Search</h2><p>Find documents by name, tag, or words inside them.</p></header>
          <section className="settings-section"><h3>Document text index</h3><p className="muted small">Rebuild the index to refresh search results for PDFs, text, and scans. Text extraction stays on this device.</p><button className="secondary" disabled={indexing} onClick={onRebuildIndex}>{indexing ? 'Indexing documents…' : 'Rebuild search index'}</button></section>
          <SemanticSettings state={state} onRefresh={onRefresh} />
        </section>}

        {(error || operationError) && <p role="alert" className="settings-feedback form-error">{error || operationError}</p>}
        {message && <p role="status" className="settings-feedback muted">{message}</p>}
      </div>
    </div>
  </main>;
}
