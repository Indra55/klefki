export type Protection = 'protected' | 'unprotected' | 'unsupported' | 'unavailable';
export async function protection(): Promise<Protection> {
    if (!navigator.storage?.persisted) return 'unsupported';
    try { return await navigator.storage.persisted() ? 'protected' : 'unprotected'; }
    catch { return 'unavailable'; }
}
export async function protectStorage(retry = false): Promise<Protection> {
    const current = await protection();
    if (current !== 'unprotected' || !navigator.storage.persist) return current;
    try {
        if (!retry && localStorage.getItem('klefki-persistence-requested')) return current;
        localStorage.setItem('klefki-persistence-requested', '1');
        return await navigator.storage.persist() ? 'protected' : 'unprotected';
    } catch { return 'unavailable'; }
}
export function storageError(error: unknown): string {
    if (error instanceof DOMException && error.name === 'QuotaExceededError') return 'Device storage is full. This change was not saved. Free space and try again.';
    return error instanceof Error ? error.message : 'Local storage is unavailable. Your existing data has not been replaced.';
}
