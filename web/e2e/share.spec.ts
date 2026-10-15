import { test, expect, type Page } from '@playwright/test';
import { readFile } from 'node:fs/promises';
import { documentAction, upload } from './ui.ts';

type SharedFile = {name: string; type: string; bytes: number[]};
declare global {
    interface Window {
        shareFixture: {
            calls: {keys: string[]; files: SharedFile[]; activated: boolean}[];
            error?: string; hold?: boolean; finish?: () => void;
        };
    }
}
const password = 'public document sharing fixture password';
async function create(page: Page, support = 'supported') {
    await page.addInitScript(support => {
        window.shareFixture = {calls: []};
        Object.defineProperty(navigator, 'canShare', {configurable: true, value: support === 'missing' ? undefined : (data: ShareData) => {
            if (support === 'throws') throw new TypeError('unsupported file');
            return support === 'supported' && !!data.files?.length;
        }});
        Object.defineProperty(navigator, 'share', {configurable: true, value: support === 'missing' ? undefined : async (data: ShareData) => {
            const activated = navigator.userActivation.isActive;
            const files = await Promise.all(data.files!.map(async file => ({name: file.name, type: file.type, bytes: Array.from(new Uint8Array(await file.arrayBuffer()))})));
            window.shareFixture.calls.push({keys: Object.keys(data), files, activated});
            if (window.shareFixture.error) throw new DOMException('public share fixture', window.shareFixture.error);
            if (window.shareFixture.hold) await new Promise<void>(resolve => { window.shareFixture.finish = resolve; });
        }});
    }, support);
    await page.goto('/');
    await page.getByLabel('Password', {exact: true}).fill(password);
    await page.getByLabel('Confirm password').fill(password);
    await page.getByRole('button', {name: 'Create Klefki', exact: true}).click();
    await page.getByRole('button', {name: 'I have saved it', exact: true}).click();
    await expect(page.locator('.library')).toBeVisible();
}

test('document sharing passes authenticated file bytes and types to the system without selecting an app', async ({page}) => {
    await page.setViewportSize({width: 390, height: 844});
    await create(page);
    const downloads: string[] = [];
    page.on('download', download => downloads.push(download.suggestedFilename()));
    for (const [name, type] of [
        ['Résumé.PDF', 'application/pdf'],
        ['report.docx', 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'],
        ['photo.jpg', 'image/jpeg'],
        ['document.unlisted', 'application/octet-stream'],
    ]) {
        const buffer = Buffer.from([0, 255, 19, 128, 42, 10]);
        await upload(page, {name, mimeType: type, buffer});
        await documentAction(page, name, 'Share');
        const dialog = page.getByRole('dialog', {name: 'Share document', exact: true});
        await expect(dialog.getByText(name, {exact: true})).toBeVisible();
        const count = await page.evaluate(() => window.shareFixture.calls.length);
        expect(count).toBe(['Résumé.PDF', 'report.docx', 'photo.jpg', 'document.unlisted'].indexOf(name));
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        await dialog.getByRole('button', {name: 'Choose app', exact: true}).click();
        await expect(dialog).toHaveCount(0);
        expect(await page.evaluate(() => window.shareFixture.calls.at(-1))).toEqual({keys: ['files'], files: [{name, type, bytes: Array.from(buffer)}], activated: true});
        await expect(page.getByRole('button', {name: 'Open ' + name, exact: true})).toBeVisible();
    }
    expect(downloads).toEqual([]);
});

test('preview sharing works offline and cancellation and errors allow retry without exporting', async ({page, context}) => {
    await create(page);
    const buffer = Buffer.from('Public offline document fixture ✓');
    await upload(page, {name: 'notes.txt', mimeType: 'text/plain', buffer});
    await page.evaluate(async () => { await navigator.serviceWorker.ready; });
    await expect.poll(() => page.evaluate(() => !!navigator.serviceWorker.controller)).toBe(true);
    await context.setOffline(true);
    await page.getByRole('button', {name: 'Open notes.txt', exact: true}).click();
    const preview = page.getByRole('dialog', {name: 'notes.txt', exact: true});
    await expect(preview.locator('pre')).toHaveText(buffer.toString());
    await preview.getByRole('button', {name: 'Share', exact: true}).click();
    const dialog = page.getByRole('dialog', {name: 'Share document', exact: true});
    const downloads: string[] = [];
    page.on('download', download => downloads.push(download.suggestedFilename()));
    for (const error of ['AbortError', 'NotAllowedError', 'DataError']) {
        await page.evaluate(error => { window.shareFixture.error = error; }, error);
        await dialog.getByRole('button', {name: 'Choose app', exact: true}).click();
        await expect(dialog.getByRole('status')).toContainText(error === 'AbortError' ? 'Sharing cancelled' : 'Could not share this document');
        await expect(dialog.getByRole('button', {name: 'Choose app', exact: true})).toBeEnabled();
    }
    expect(downloads).toEqual([]);
    await page.evaluate(() => { window.shareFixture.error = undefined; });
    await dialog.getByRole('button', {name: 'Choose app', exact: true}).click();
    await expect(dialog).toHaveCount(0);
    await expect(preview.locator('pre')).toHaveText(buffer.toString());
    const calls = await page.evaluate(() => window.shareFixture.calls);
    expect(calls).toHaveLength(4);
    for (const call of calls) expect(call.files).toEqual([{name: 'notes.txt', type: 'text/plain', bytes: Array.from(buffer)}]);
});

for (const support of ['missing', 'rejected', 'throws']) test(`unsupported sharing (${support}) offers an explicit export with original bytes`, async ({page}) => {
    await create(page, support);
    const buffer = Buffer.from([0, 255, 128, 42]);
    await upload(page, {name: 'archive.zip', mimeType: 'application/zip', buffer});
    await documentAction(page, 'archive.zip', 'Share');
    const dialog = page.getByRole('dialog', {name: 'Share document', exact: true});
    await expect(dialog.getByRole('status')).toContainText('File sharing is unavailable');
    await expect(dialog.getByRole('button', {name: 'Choose app', exact: true})).toBeHidden();
    const pending = page.waitForEvent('download');
    await dialog.getByRole('button', {name: 'Export', exact: true}).click();
    const download = await pending;
    expect(download.suggestedFilename()).toBe('archive.zip');
    expect(await readFile((await download.path())!)).toEqual(buffer);
    await expect(dialog).toHaveCount(0);
    expect(await page.evaluate(() => window.shareFixture.calls)).toEqual([]);
});

test('cancel and lock discard prepared shares and late native completion leaves the vault locked', async ({page}) => {
    await create(page);
    await upload(page, {name: 'private.txt', mimeType: 'text/plain', buffer: Buffer.from('public lock fixture')});
    await documentAction(page, 'private.txt', 'Share');
    const dialog = page.getByRole('dialog', {name: 'Share document', exact: true});
    await dialog.getByRole('button', {name: 'Cancel', exact: true}).click();
    await expect(dialog).toHaveCount(0);
    expect(await page.evaluate(() => window.shareFixture.calls)).toEqual([]);
    await documentAction(page, 'private.txt', 'Share');
    await page.keyboard.press('Control+Shift+L');
    await expect(dialog).toHaveCount(0);
    expect(await page.evaluate(() => window.shareFixture.calls)).toEqual([]);
    await page.getByLabel('Password', {exact: true}).fill(password);
    await page.getByRole('button', {name: 'Unlock Klefki', exact: true}).click();
    await expect(dialog).toHaveCount(0);
    await documentAction(page, 'private.txt', 'Share');
    await page.evaluate(() => { window.shareFixture.hold = true; });
    await dialog.getByRole('button', {name: 'Choose app', exact: true}).click();
    await expect(dialog.getByRole('button', {name: 'Choose app', exact: true})).toBeDisabled();
    await expect.poll(() => page.evaluate(() => !!window.shareFixture.finish)).toBe(true);
    await page.keyboard.press('Control+Shift+L');
    await expect(dialog).toHaveCount(0);
    await page.evaluate(() => window.shareFixture.finish!());
    await expect(page.getByRole('button', {name: 'Unlock Klefki', exact: true})).toBeVisible();
    await expect(page.locator('dialog')).toHaveCount(0);
    await expect(page.locator('body')).not.toContainText('private.txt');
});
