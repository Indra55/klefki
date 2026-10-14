import {syncLabel,deviceName,receipt,presence,type SyncInfo} from '../../ui/sync-status.ts';
import { node, button, field, iconButton, preferences, savePreferences, type Preferences } from './ui.ts';
export type Section = 'appearance' | 'privacy' | 'sync' | 'search';
export type SettingsActions = {
    pause:()=>void;syncInfo:SyncInfo;aliases:Record<string,string>;rename:(id:string,name:string)=>Promise<void>;
    section: Section; account: string; pending: number; mode: string; selectedMode: string; selectMode: (mode:string)=>void; devices: string[]; device?: string;
    changeSection: (section: Section) => void; back: () => void; sync: (register?: boolean) => void;
    exportAccount: () => void; setMode: (mode: string) => void; forget: (id: string) => void;
    addPasskey: (panel: HTMLElement) => void; addSemantic: (panel: HTMLElement) => void;
    preferenceChanged: () => void;
};
export function settingsPage(actions: SettingsActions) {
    const main = node('main', undefined, 'settings-page');
    main.append(iconButton('Back to library', 'back', actions.back, 'quiet settings-back'));
    const heading = node('header', undefined, 'settings-heading'), title = node('h1', 'Settings'); title.tabIndex = -1;
    heading.append(title, node('p', 'Make Klefki feel at home. Preferences are saved on this device.'));
    const layout = node('div', undefined, 'settings-layout'), nav = node('nav', undefined, 'settings-nav'), panel = node('div', undefined, 'settings-panel');
    nav.setAttribute('aria-label', 'Settings sections');
    const sections = [ ['appearance', 'Appearance', 'palette'], ['privacy', 'Privacy & lock', 'lock'], ['sync', 'Sync & devices', 'cloud'], ['search', 'Search', 'search'] ] as const;
    for (const [id, label, symbol] of sections) {
        const action = iconButton(label, symbol, () => actions.changeSection(id));
        if (id === actions.section) action.setAttribute('aria-current', 'page');
        nav.append(action);
    }
    layout.append(nav, panel); main.append(heading, layout);
    const current = preferences();
    function panelHeading(title: string, description: string) {
        const header = node('header', undefined, 'settings-panel-heading'); header.append(node('h2', title), node('p', description)); panel.append(header);
    }
    function preference<K extends keyof Preferences>(label: string, description: string, key: K, options: [string, string][]) {
        const row = node('div', undefined, 'preference-row'), copy = node('div'), caption = node('label', label), select = node('select');
        select.id = 'preference-' + key; caption.htmlFor = select.id;
        for (const [value, name] of options) { const option = node('option', name); option.value = value; select.append(option); }
        select.value = String(current[key]);
        select.onchange = () => {
            const value = key === 'idleMinutes' ? Number(select.value) : select.value;
            savePreferences({...current, [key]: value}); actions.preferenceChanged();
        };
        copy.append(caption, node('p', description)); row.append(copy, select); panel.append(row);
    }
    if (actions.section === 'appearance') {
        panelHeading('Appearance', 'Choose how your library looks and how you browse it.');
        preference('Color theme', 'Use a light or dark theme, or follow your device.', 'theme', [['dark','Dark'],['light','Light'],['system','Follow system']]);
        preference('Document spacing', 'Give documents room to breathe, or fit more rows on screen.', 'density', [['comfortable','Comfortable'],['compact','Compact']]);
        preference('Default sort order', 'Start with this order when you open the library.', 'defaultSort', [['name','Name'],['type','Type'],['size','Size, largest first']]);
    }
    if (actions.section === 'privacy') {
        panelHeading('Privacy & lock', 'Choose when to lock your library and how to unlock it.');
        preference('Lock after inactivity', 'Locking clears active keys and document previews.', 'idleMinutes', [1,5,15,30].map(n => [String(n), n === 1 ? '1 minute' : n + ' minutes']));
        panel.append(node('p', 'Lock immediately with Ctrl Shift L.', 'settings-hint')); actions.addPasskey(panel);
    }
    if (actions.section === 'sync') {
        panelHeading('Sync & devices', 'Keep the same encrypted library on your devices. You can still work offline.');
        const state = node('div', undefined, 'connection-status'), copy = node('div');
        const statusLabel=node('span',syncLabel(actions.syncInfo,actions.device||'',actions.aliases));statusLabel.id='settings-sync-label';copy.append(statusLabel, node('p', actions.pending ? actions.pending + ' changes waiting to sync.' : 'No local changes waiting to sync.'));
        const start=button('Sync now',()=>actions.sync(),'secondary');start.dataset.syncStart='';start.disabled=!!actions.syncInfo.paused||actions.syncInfo.running;
        const pause=button(actions.syncInfo.paused?'Resume sync':'Pause sync',actions.pause,'secondary');pause.dataset.syncPause='';
        state.append(copy,start,pause); panel.append(state,node('p','Pause syncing on this device while adding files. Local changes stay queued; an active sync finishes. Resume when the batch is ready. Concurrent edits on other devices can still create conflicts.','settings-hint'));
        const address = field('Server address', 'url'); address.input.value = location.origin; address.input.readOnly = true;
        const register=button('Register this store on server',()=>actions.sync(true),'secondary');register.dataset.syncStart='';register.disabled=!!actions.syncInfo.paused||actions.syncInfo.running;
        panel.append(address.wrap,node('p','This browser connects to the server hosting Klefki. Use this address on your other devices.','settings-hint'),register);
        const storage = node('section', undefined, 'settings-section'), row = node('div', undefined, 'preference-row'), info = node('div'), caption = node('label', 'Sync mode'), select = node('select');
        select.id = 'sync-mode'; caption.htmlFor = select.id;
        for (const [value,label] of [['persistent','Persistent storage'],['temporary-relay','Temporary relay']]) { const option = node('option',label); option.value = value; select.append(option); }
        select.value = actions.selectedMode;
        info.append(caption, node('p', 'Shared by every device linked to this account.')); row.append(info, select);
        storage.append(node('h3', 'Server storage'), row, node('p', actions.mode === 'temporary-relay' ? 'The server removes delivered documents after every linked device has saved them, with at least two devices required. Offline devices keep deliveries queued. Keep a local backup. New devices need an existing device to reconnect.' : 'Keep encrypted documents on the server so devices can download them later.', 'muted small'));
        const apply = button('Apply sync mode', () => actions.setMode(select.value), 'secondary'); apply.disabled = select.value === actions.mode;
        select.onchange = () => { actions.selectMode(select.value); apply.disabled = select.value === actions.mode; };
        storage.append(apply, node('p', 'Free hosts can erase queued deliveries on restart. Existing devices retain their copies and supply them again when they reconnect.', 'muted small')); panel.append(storage);
        const device = node('section', undefined, 'settings-section');
        device.append(node('h3', 'Set up another device'), node('p', 'Save your encrypted account file, open it on the other device, and use your password and the same server.', 'muted small'), iconButton('Save encrypted account file', 'download', actions.exportAccount));
        const details = node('details', undefined, 'settings-details'); details.append(node('summary', 'Account and local storage'));
        const list = node('dl'); list.append(node('dt', 'Account ID'), node('dd', actions.account, 'account-id'), node('dt', 'Document store'), node('dd', 'Encrypted storage in this browser'));
        details.append(list); device.append(details, node('p', 'Keep the account file and recovery code safe. Browser storage can be cleared or evicted.', 'muted small')); panel.append(device);
        if (actions.devices.length) {
            const linked = node('section', undefined, 'settings-section'); linked.append(node('h3', 'Linked devices'), node('p', 'Forget a device you no longer use so it stops holding relay deliveries. It will need a fresh local store to join again.', 'muted small'));
            for (const id of actions.devices) {
                const row = node('div', undefined, 'preference-row'); const name=node('span',deviceName(id,actions.aliases,actions.syncInfo.peers?.devices,actions.device));name.dataset.syncDevice=id;row.append(name);
                if(id!==actions.device){const forget=button('Forget device',()=>actions.forget(id),'quiet');forget.dataset.syncStart='';forget.disabled=!!actions.syncInfo.paused;row.append(forget);}linked.append(row);
                const status=node('p',undefined,'muted small');status.dataset.syncReceipt=id;const peer=actions.syncInfo.peers?.devices.find(d=>d.device===id);status.textContent=peer?presence(peer)+' · '+receipt(peer,actions.syncInfo.statusAvailable):'Status unknown';linked.append(status);
                const form=node('form',undefined,'sync-device'),nickname=node('input'),save=node('button','Save nickname','quiet');nickname.setAttribute('aria-label','Nickname for device '+id.slice(0,8));nickname.value=actions.aliases[id]||'';nickname.placeholder=name.textContent||'';nickname.maxLength=64;save.type='submit';form.append(nickname,save);form.onsubmit=e=>{e.preventDefault();save.disabled=true;void actions.rename(id,nickname.value).catch(()=>{status.textContent='Could not save this nickname.'}).finally(()=>{save.disabled=false})};linked.append(form);
            }
            panel.append(linked);
        }
    }
    if (actions.section === 'search') {
        panelHeading('Search', 'Find documents by name, tag, or words inside them.');
        const indexing = node('section', undefined, 'settings-section'); indexing.append(node('h3', 'Document text index'), node('p', 'Build or rebuild the document text index on desktop or CLI, then sync it to this browser. Names and tags are always searchable.', 'muted small')); panel.append(indexing);
        actions.addSemantic(panel);
    }
    return main;
}
