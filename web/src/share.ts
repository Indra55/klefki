import { node, button, iconButton } from './ui.ts';

const active = new Set<() => void>();
export function closeShares() { for (const close of active) close(); }

const types: Record<string, string> = {
    pdf: 'application/pdf', txt: 'text/plain', md: 'text/markdown', csv: 'text/csv',
    json: 'application/json', xml: 'application/xml', html: 'text/html', rtf: 'application/rtf',
    doc: 'application/msword', docx: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    xls: 'application/vnd.ms-excel', xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    ppt: 'application/vnd.ms-powerpoint', pptx: 'application/vnd.openxmlformats-officedocument.presentationml.presentation',
    odt: 'application/vnd.oasis.opendocument.text', ods: 'application/vnd.oasis.opendocument.spreadsheet',
    odp: 'application/vnd.oasis.opendocument.presentation', zip: 'application/zip',
    png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif',
    webp: 'image/webp', svg: 'image/svg+xml', heic: 'image/heic', tif: 'image/tiff', tiff: 'image/tiff',
    mp3: 'audio/mpeg', m4a: 'audio/mp4', wav: 'audio/wav', mp4: 'video/mp4', webm: 'video/webm',
};

export function showShare(name: string, bytes: Uint8Array, download: (file: File) => void) {
    closeShares();
    const extension = name.split('.').pop()!.toLowerCase();
    let file: File | undefined = new File([bytes as Uint8Array<ArrayBuffer>], name, {
        type: Object.hasOwn(types, extension) ? types[extension] : 'application/octet-stream',
    });
    let supported = false, sharing = false;
    try { supported = isSecureContext && typeof navigator.share === 'function' && typeof navigator.canShare === 'function' && navigator.canShare({files: [file]}); } catch {}
    const dialog = node('dialog', undefined, 'modal share');
    dialog.setAttribute('aria-label', 'Share document');
    const status = node('p', supported ? 'Choose an app to receive a copy of this document.' : 'File sharing is unavailable for this file or browser. Export it to share from your device.', 'muted');
    status.setAttribute('role', 'status');
    const choose = iconButton('Choose app', 'share', () => {
        if (!file || sharing) return;
        sharing = true; choose.disabled = true;
        // Keep the native share call inside this click, after decryption is complete.
        void (async () => {
            try {
                await navigator.share({files: [file!]});
                if (file) cleanup();
            } catch (error) {
                if (file) status.textContent = error instanceof DOMException && error.name === 'AbortError'
                    ? 'Sharing cancelled. Choose an app to try again.'
                    : 'Could not share this document. Try again or export it.';
            } finally { sharing = false; if (file) choose.disabled = false; }
        })();
    }, 'primary');
    choose.hidden = !supported;
    const actions = node('div', undefined, 'form-actions');
    actions.append(button('Cancel', () => cleanup(), 'quiet'), button('Export', () => {
        if (file) download(file);
        cleanup();
    }), choose);
    dialog.append(node('h2', 'Share document'), node('p', name, 'share-name'), status, actions);
    const cleanup = () => {
        if (!file) return;
        file = undefined; observer.disconnect(); active.delete(cleanup); dialog.close(); dialog.remove();
    };
    dialog.onclose = cleanup;
    const observer = new MutationObserver(() => { if (!dialog.isConnected) cleanup(); });
    active.add(cleanup);
    const root = document.querySelector('#app')!;
    root.append(dialog); dialog.showModal(); observer.observe(root, {childList: true, subtree: true});
    if (supported) choose.focus();
}
