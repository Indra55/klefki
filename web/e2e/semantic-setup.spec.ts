import { test, expect, type Page } from '@playwright/test';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { openSettings } from './ui.ts';

const password = 'public semantic setup fixture password';
test.beforeEach(() => {
    test.skip(!existsSync('dist/models/bge-small/onnx/model.onnx'), 'Requires the browser model assets');
});
async function create(page: Page) {
    await page.goto('/');
    await page.getByLabel('Password', { exact: true }).fill(password);
    await page.getByLabel('Confirm password').fill(password);
    await page.getByRole('button', { name: 'Create Klefki', exact: true }).click();
    await page.getByRole('button', { name: 'I have saved it' }).click();
    await openSettings(page, 'Sync & devices');
    await page.getByRole('button', { name: 'Register this store on server', exact: true }).click();
    await expect(page.getByRole('status')).toContainText('Synced.');
    await page.getByRole('button', { name: 'Back to library', exact: true }).click();
    await openSettings(page, 'Search');
}
async function checkModel(page: Page) {
    const worker = readdirSync('dist/assets').find(name => /^semantic-worker-.*\.js$/.test(name))!;
    const parity = JSON.parse(readFileSync('tests/m8-parity.json', 'utf8'));
    const result = await page.evaluate(({ worker, text }) => new Promise<any>((resolve, reject) => {
        const w = new Worker('/assets/' + worker, { type: 'module' });
        w.onerror = reject;
        w.onmessage = event => {
            w.terminate();
            event.data.error ? reject(Error(event.data.error)) : resolve(event.data.result);
        };
        w.postMessage({ id: 1, text });
    }), { worker, text: parity.text });
    expect(result.tokens).toEqual(parity.tokens);
    expect(result.fingerprint).toBe(parity.fingerprint);
    expect(Math.max(...result.vector.map((value: number, i: number) => Math.abs(value - parity.vector[i])))).toBeLessThan(0.0001);
}
for (const standalone of [false, true]) {
test(`real model installs and runs offline in ${standalone ? 'standalone PWA mode' : 'browser mode'}`, async ({ page, context }) => {
    if (standalone) {
        await page.setViewportSize({ width: 390, height: 844 });
        await page.addInitScript(() => {
            const matchMedia = window.matchMedia.bind(window);
            window.matchMedia = query => query === '(display-mode: standalone)'
                ? Object.assign(matchMedia(query), { matches: true }) : matchMedia(query);
        });
    }
    await create(page);
    const requests: string[] = [];
    page.on('request', request => { if (request.url().includes('/models/bge-small/')) requests.push(request.url()); });
    await page.getByRole('button', { name: 'Install BGE-small model', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Cancel setup', exact: true })).toBeHidden({ timeout: 60000 });
    await expect(page.getByRole('button', { name: 'BGE-small model installed', exact: true })).toBeDisabled();
    await expect(page.getByLabel('Semantic search and suggestions on this device', { exact: true })).toBeChecked();
    await checkModel(page);
    await page.evaluate(async () => { await navigator.serviceWorker.ready; });
    await expect.poll(() => page.evaluate(() => !!navigator.serviceWorker.controller)).toBe(true);
    await context.setOffline(true);
    await page.reload();
    await page.getByLabel('Password', { exact: true }).fill(password);
    await page.getByRole('button', { name: 'Unlock Klefki', exact: true }).click();
    await openSettings(page, 'Search');
    await expect(page.getByRole('button', { name: 'BGE-small model installed', exact: true })).toBeDisabled();
    await expect(page.getByLabel('Semantic search and suggestions on this device', { exact: true })).toBeChecked();
    const count = requests.length;
    await page.getByLabel('Semantic search and suggestions on this device', { exact: true }).uncheck();
    await expect(page.getByRole('button', { name: 'BGE-small model installed', exact: true })).toBeDisabled();
    await page.getByLabel('Semantic search and suggestions on this device', { exact: true }).check();
    await expect(page.getByRole('button', { name: 'Cancel setup', exact: true })).toBeHidden({ timeout: 60000 });
    await expect(page.getByLabel('Semantic search and suggestions on this device', { exact: true })).toBeChecked();
    await checkModel(page);
    expect(requests.length).toBe(count);
});
}

test.describe('runtime recovery', () => {
test.use({ serviceWorkers: 'block' });
test('a runtime load failure can retry using the downloaded model', async ({ page, context }) => {
    await create(page);
    let requests = 0;
    await context.route('**/models/runtime/*.mjs', route => ++requests === 2 ? route.abort() : route.continue());
    await page.getByRole('button', { name: 'Install BGE-small model', exact: true }).click();
    await expect.poll(() => requests).toBe(2);
    await expect(page.getByText('Model verification or inference failed.', { exact: true })).toBeVisible({ timeout: 60000 });
    await expect(page.getByRole('button', { name: 'Install BGE-small model', exact: true })).toBeEnabled();
    await expect(page.getByLabel('Semantic search and suggestions on this device', { exact: true })).not.toBeChecked();
    await context.unroute('**/models/runtime/*.mjs');
    await page.getByRole('button', { name: 'Install BGE-small model', exact: true }).click();
    await expect(page.getByRole('button', { name: 'BGE-small model installed', exact: true })).toBeDisabled({ timeout: 60000 });
    await checkModel(page);
});
});
