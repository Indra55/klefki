import { node, iconButton } from './ui.ts';
import type { PDFDocumentProxy, RenderTask, PDFDocumentLoadingTask } from 'pdfjs-dist';

const active = new Set<() => void>();
export function closePreviews() { for (const close of active) close(); }

export async function showPreview(name: string, bytes: Uint8Array, closed: () => void, download: () => void) {
    const dialog = node('dialog', undefined, 'modal wide preview');
    dialog.setAttribute('aria-label', name);
    const header = node('header'), title = node('h2', name);
    const close = iconButton('', 'close', () => dialog.close(), 'icon'); close.setAttribute('aria-label', 'Close');
    header.append(title, close);
    const toolbar = node('div', undefined, 'preview-toolbar'), controls = node('div');
    toolbar.append(node('span', 'Document', 'muted'), controls);
    const content = node('div', undefined, 'preview-content');
    dialog.append(header, toolbar, content);
    let alive = true, objectURL: string | undefined, document: PDFDocumentProxy | undefined, task: PDFDocumentLoadingTask | undefined, render: RenderTask | undefined;
    let page = 1, zoom = 1, rendering = 0;
    const cleanup = () => {
        if (!alive) return;
        alive = false; active.delete(cleanup); observer.disconnect(); render?.cancel(); void task?.destroy();
        if (objectURL) URL.revokeObjectURL(objectURL);
        dialog.remove(); closed();
    };
    dialog.onclose = cleanup;
    const observer = new MutationObserver(() => { if (!dialog.isConnected) { cleanup(); observer.disconnect(); } });
    active.add(cleanup);
    controls.append(iconButton('Export', 'download', download));
    const root = globalThis.document.querySelector('#app')!;
    root.append(dialog); dialog.showModal(); observer.observe(root, {childList: true, subtree: true});
    const pdf = /\.pdf$/i.test(name), image = /\.(png|jpe?g|gif|webp)$/i.test(name), text = /\.(txt|md|csv|json)$/i.test(name);
    const zoomLabel = node('span', '100%'), pageLabel = node('span');
    const previous = iconButton('', 'back', () => { page--; void draw(); }, 'icon'); previous.setAttribute('aria-label', 'Previous page');
    const next = iconButton('', 'chevron', () => { page++; void draw(); }, 'icon'); next.setAttribute('aria-label', 'Next page');
    previous.disabled = next.disabled = true;
    const canvas = node('canvas');
    async function draw() {
        const sequence = ++rendering; render?.cancel();
        zoomLabel.textContent = Math.round(zoom * 100) + '%';
        if (!document || !alive) return;
        previous.disabled = page <= 1; next.disabled = page >= document.numPages;
        pageLabel.textContent = page + ' / ' + document.numPages;
        try {
            const sheet = await document.getPage(page);
            if (!alive || sequence !== rendering) return;
            const viewport = sheet.getViewport({scale: zoom * 1.25});
            canvas.width = viewport.width; canvas.height = viewport.height;
            canvas.setAttribute('aria-label', 'Page ' + page + ' of ' + name);
            render = sheet.render({canvas, viewport}); await render.promise;
        } catch (error) {
            if (alive && sequence === rendering && (error as Error).name !== 'RenderingCancelledException')
                content.replaceChildren(node('p', 'This page could not be displayed. Export the document to open it on your device.', 'muted'));
        }
    }
    if (pdf || image) {
        if (pdf) controls.prepend(previous, pageLabel, next);
        const out = iconButton('−', 'search', () => { zoom = Math.max(.5, zoom - .25); updateZoom(); }, 'icon'); out.setAttribute('aria-label', 'Zoom out'); out.replaceChildren(globalThis.document.createTextNode('−'));
        const into = iconButton('+', 'plus', () => { zoom = Math.min(3, zoom + .25); updateZoom(); }, 'icon'); into.setAttribute('aria-label', 'Zoom in');
        controls.append(out, zoomLabel, into);
    }
    function updateZoom() {
        zoomLabel.textContent = Math.round(zoom * 100) + '%';
        const img = content.querySelector('img'); if (img) img.style.width = zoom * 100 + '%';
        if (pdf) void draw();
    }
    try {
        if (pdf) {
            content.append(node('p', 'Opening your document…', 'muted'));
            const {getDocument, GlobalWorkerOptions} = await import('pdfjs-dist');
            const {default: workerURL} = await import('pdfjs-dist/build/pdf.worker.min.mjs?url');
            if (!alive) return;
            GlobalWorkerOptions.workerSrc = workerURL;
            task = getDocument({data: bytes.slice(), isEvalSupported: false, disableAutoFetch: true});
            document = await task.promise;
            if (!alive) return;
            content.replaceChildren(canvas); await draw();
        } else if (text) {
            content.append(node('pre', new TextDecoder('utf-8', {fatal: true}).decode(bytes)));
        } else if (image) {
            const extension = name.split('.').pop()!.toLowerCase(), mime = extension === 'jpg' || extension === 'jpeg' ? 'image/jpeg' : 'image/' + extension;
            objectURL = URL.createObjectURL(new Blob([bytes as Uint8Array<ArrayBuffer>], {type: mime}));
            const img = node('img'); img.src = objectURL; img.alt = name; img.style.width = '100%'; content.append(img);
        } else content.append(node('p', 'A preview is not available for this file. Export it to open it on your device.', 'muted'));
    } catch {
        if (alive) content.replaceChildren(node('p', 'This document could not be previewed. Export it to open it on your device.', 'muted'));
    }
}
