export const node = <K extends keyof HTMLElementTagNameMap>(tag: K, text?: string, className?: string) => {
    const element = document.createElement(tag);
    if (text) element.textContent = text;
    if (className) element.className = className;
    return element;
};
export const button = (text: string, run: () => void, className = 'secondary') => {
    const element = node('button', text, className);
    element.type = 'button';
    element.onclick = run;
    return element;
};
export function field(label: string, type = 'text') {
    const wrap = node('label', undefined, type === 'checkbox' ? 'checkbox' : ''), input = node('input');
    input.type = type;
    if (type === 'checkbox') wrap.append(input, document.createTextNode(label));
    else wrap.append(document.createTextNode(label), input);
    return { wrap, input };
}
const paths: Record<string, string[]> = {
    settings: ['M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8', 'm9 3-1 3-3 1-2 3 2 2-1 3 3 2 3-1 2 2 3-2 1-3 3-1 1-3-2-2 1-3-3-2-3 1-2-2Z'],
    lock: ['M5 10h14v11H5z', 'M8 10V6a4 4 0 0 1 8 0v4'],
    plus: ['M12 5v14M5 12h14'],
    folder: ['M3 7V5h6l2 2h10v13H3Z'],
    folderPlus: ['M3 7V5h6l2 2h10v13H3Z', 'M12 10v7M8.5 13.5h7'],
    star: ['m12 3 2.8 5.7 6.2.9-4.5 4.4 1.1 6.2L12 17.3l-5.6 2.9 1.1-6.2L3 9.6l6.2-.9Z'],
    more: ['M4 12h.01M12 12h.01M20 12h.01'],
    chevron: ['m9 5 7 7-7 7'],
    down: ['m6 9 6 6 6-6'],
    back: ['M19 12H5m7-7-7 7 7 7'],
    close: ['m6 6 12 12M6 18 18 6'],
    download: ['M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5'],
    search: ['M10 3a7 7 0 1 0 0 14 7 7 0 0 0 0-14', 'm15 15 6 6'],
    palette: ['M12 3a9 9 0 1 0 0 18h2a2 2 0 0 0 1-4l-1-1a2 2 0 0 1 2-3h2a3 3 0 0 0 3-3 9 9 0 0 0-9-7Z', 'M7 10h.01M10 6h.01M15 6h.01M18 10h.01'],
    cloud: ['M6 18a4 4 0 0 1-1-8 7 7 0 0 1 14-1 4.5 4.5 0 0 1-1 9Z'],
};
export function icon(name: string, size = 17, filled = false) {
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('viewBox', '0 0 24 24');
    svg.setAttribute('width', String(size)); svg.setAttribute('height', String(size));
    svg.setAttribute('fill', filled ? 'currentColor' : 'none');
    svg.setAttribute('stroke', 'currentColor'); svg.setAttribute('stroke-width', '1.6');
    svg.setAttribute('stroke-linecap', 'round'); svg.setAttribute('stroke-linejoin', 'round');
    svg.setAttribute('aria-hidden', 'true');
    for (const d of paths[name] || paths.more) {
        const path = document.createElementNS(svg.namespaceURI, 'path'); path.setAttribute('d', d); svg.append(path);
    }
    return svg;
}
export function iconButton(text: string, name: string, run: () => void, className = 'quiet') {
    const element = button(text, run, className); element.prepend(icon(name)); return element;
}
export function menu(label: string, choices: {label: string; run: () => void; danger?: boolean}[], symbol?: string) {
    const details = node('details', undefined, 'dropdown'), trigger = node('summary', symbol ? undefined : label, symbol ? 'icon' : 'quiet menu-trigger');
    trigger.setAttribute('role', 'button'); trigger.setAttribute('aria-label', label);
    trigger.setAttribute('aria-haspopup', 'menu'); trigger.setAttribute('aria-expanded', 'false');
    if (symbol) trigger.append(icon(symbol, 20)); else trigger.append(icon('down', 12));
    const panel = node('div', undefined, 'menu'); panel.setAttribute('role', 'menu');
    for (const choice of choices) {
        const action = button(choice.label, () => { details.open = false; trigger.focus(); choice.run(); }, 'menu-item' + (choice.danger ? ' danger' : ''));
        action.setAttribute('role', 'menuitem'); panel.append(action);
    }
    details.append(trigger, panel);
    details.addEventListener('toggle', () => {
        trigger.setAttribute('aria-expanded', String(details.open));
        if (details.open) positionMenu(details);
    });
    details.onkeydown = event => {
        const items = [...panel.querySelectorAll<HTMLButtonElement>('button')];
        if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); details.open = false; trigger.focus(); }
        if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            event.preventDefault(); details.open = true;
            positionMenu(details);
            const step = event.key === 'ArrowDown' ? 1 : -1, index = items.indexOf(document.activeElement as HTMLButtonElement);
            items[(index < 0 ? (step === 1 ? 0 : items.length - 1) : (index + step + items.length) % items.length)]?.focus();
        }
    };
    return details;
}
function positionMenu(details: HTMLDetailsElement) {
    const trigger = details.querySelector('summary')!, panel = details.querySelector<HTMLElement>('.menu')!;
    const viewport = window.visualViewport, margin = 12, gap = 8;
    const left = (viewport?.offsetLeft || 0) + margin, top = (viewport?.offsetTop || 0) + margin;
    const width = Math.max(0, (viewport?.width || innerWidth) - margin * 2);
    const height = Math.max(0, (viewport?.height || innerHeight) - margin * 2);
    panel.style.maxWidth = `${Math.min(300, width)}px`; panel.style.minWidth = `${Math.min(180, width)}px`;
    panel.style.maxHeight = `${height}px`;
    const anchor = trigger.getBoundingClientRect(), rect = panel.getBoundingClientRect();
    const below = top + height - anchor.bottom - gap, above = anchor.top - gap - top;
    const y = rect.height <= below || below >= above ? anchor.bottom + gap : anchor.top - gap - rect.height;
    panel.style.left = `${Math.max(left, Math.min(anchor.right - rect.width, left + width - rect.width))}px`;
    panel.style.top = `${Math.max(top, Math.min(y, top + height - rect.height))}px`;
}
function positionOpenMenus() {
    for (const details of document.querySelectorAll<HTMLDetailsElement>('.dropdown[open]')) positionMenu(details);
}
window.addEventListener('resize', positionOpenMenus);
document.addEventListener('scroll', positionOpenMenus, true);
window.visualViewport?.addEventListener('resize', positionOpenMenus);
window.visualViewport?.addEventListener('scroll', positionOpenMenus);
document.addEventListener('pointerdown', event => {
    for (const details of document.querySelectorAll<HTMLDetailsElement>('.dropdown[open]'))
        if (!details.contains(event.target as Node)) details.open = false;
});

export type Preferences = {theme: 'dark' | 'light' | 'system'; density: 'comfortable' | 'compact'; defaultSort: 'name' | 'type' | 'size'; idleMinutes: number};
const defaults: Preferences = {theme: 'dark', density: 'comfortable', defaultSort: 'name', idleMinutes: 5};
export function preferences(): Preferences {
    try {
        const saved = JSON.parse(localStorage.getItem('klefki-appearance-v1') || '{}');
        return {
            theme: ['dark', 'light', 'system'].includes(saved.theme) ? saved.theme : defaults.theme,
            density: saved.density === 'compact' ? 'compact' : defaults.density,
            defaultSort: ['name', 'type', 'size'].includes(saved.defaultSort) ? saved.defaultSort : defaults.defaultSort,
            idleMinutes: [1, 5, 15, 30].includes(saved.idleMinutes) ? saved.idleMinutes : defaults.idleMinutes,
        };
    } catch { return {...defaults}; }
}
export function applyPreferences() {
    const theme = preferences().theme;
    document.documentElement.dataset.theme = theme === 'system' ? (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light') : theme;
    document.querySelector('meta[name=theme-color]')?.setAttribute('content', document.documentElement.dataset.theme === 'dark' ? '#111214' : '#f7f7f5');
}
export function savePreferences(value: Preferences) {
    localStorage.setItem('klefki-appearance-v1', JSON.stringify(value)); applyPreferences();
}
matchMedia('(prefers-color-scheme: dark)').addEventListener('change', applyPreferences);
applyPreferences();
