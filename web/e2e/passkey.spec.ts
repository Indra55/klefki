import { openSettings, upload } from './ui.ts';
import { test, expect, type Page, type BrowserContext } from '@playwright/test';
import * as c from '../src/crypto.ts';

const password = 'public passkey browser fixture password';
test.use({ baseURL: 'http://localhost:5188' });
const recordKey = async (page: Page) => page.evaluate(() => 'klefki-passkey-v1-' + JSON.parse(localStorage.getItem('klefki-account')!).account);
async function authenticator(context: BrowserContext, page: Page, hasPrf = true, transport: 'internal' | 'usb' = 'internal') {
    const session = await context.newCDPSession(page);
    await session.send('WebAuthn.enable');
    const { authenticatorId } = await session.send('WebAuthn.addVirtualAuthenticator', { options: {
        protocol: 'ctap2', ctap2Version: 'ctap2_1', transport, hasResidentKey: true,
        hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true, hasPrf,
    } });
    return { session, authenticatorId };
}
async function create(page: Page) {
    await page.goto('/');
    await page.getByLabel('Password', { exact: true }).fill(password);
    await page.getByLabel('Confirm password').fill(password);
    await page.getByRole('button', { name: 'Create Klefki', exact: true }).click();
    await page.getByRole('button', { name: 'I have saved it' }).click();
    await expect(page.getByRole('heading', { name: 'Documents', exact: true })).toBeVisible();
}
async function settings(page: Page) { await openSettings(page, 'Privacy & lock'); }
async function enable(page: Page) {
    await settings(page);
    await page.getByRole('button', { name: 'Enable passkey unlock', exact: true }).click();
    await expect(page.getByText('Passkey unlock is enabled in this browser.', { exact: true })).toBeVisible();
}
async function passwordUnlock(page: Page) {
    await page.getByLabel('Password', { exact: true }).fill(password);
    await page.getByRole('button', { name: 'Unlock Klefki', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Documents', exact: true })).toBeVisible();
}
async function lock(page: Page) { await page.getByRole('button', { name: 'Lock', exact: true }).click(); }
async function passkeyUnlock(page: Page) {
    await page.getByRole('button', { name: 'Unlock with passkey', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Documents', exact: true })).toBeVisible();
}

test('PRF passkey unlock persists, works offline, and retains the sync credential', async ({ page, context }) => {
    await authenticator(context, page);
    await create(page);
    await upload(page, { name: 'private.txt', mimeType: 'text/plain', buffer: Buffer.from('private passkey fixture document') });
    await expect(page.getByText('private.txt', { exact: true })).toBeVisible();
    await enable(page);
    const persisted = await page.evaluate(() => Object.fromEntries(Object.entries(localStorage)));
    await c.ready;
    const account = c.validateAccount(JSON.parse(persisted['klefki-account']));
    const keys = c.unlock(account, password), raw = JSON.stringify(persisted);
    try {
        for (const secret of [password, 'private passkey fixture document', c.base64(keys.master), c.hex(keys.master), c.base64(keys.credential), c.hex(keys.credential)])
            expect(raw).not.toContain(secret);
    }
    finally { c.wipe(keys.master); c.wipe(keys.credential); }
    await page.evaluate(async () => { await navigator.serviceWorker.ready; });
    await expect.poll(() => page.evaluate(() => !!navigator.serviceWorker.controller)).toBe(true);
    await context.setOffline(true);
    await page.reload();
    await passkeyUnlock(page);
    await page.getByRole('button', { name: 'Open private.txt', exact: true }).click();
    await expect(page.locator('dialog pre')).toHaveText('private passkey fixture document');
    await page.getByRole('button', { name: 'Close', exact: true }).click();
    await lock(page);
    await expect(page.locator('dialog')).toHaveCount(0);
    await expect(page.getByText('private.txt', { exact: true })).toHaveCount(0);
    await passkeyUnlock(page);
    await lock(page);
    await context.setOffline(false);
    await passkeyUnlock(page);
    await settings(page);
    await openSettings(page, 'Sync & devices');
    await page.getByRole('button', { name: 'Register this store on server', exact: true }).click();
    await expect(page.getByRole('status')).toContainText('Synced.');
    await lock(page);
    await passwordUnlock(page);
    await expect(page.getByText('private.txt', { exact: true })).toBeVisible();
});

test('desktop web enables and unlocks with a PRF security key without a platform authenticator',async({page,context})=>{
 await page.addInitScript(()=>{PublicKeyCredential.isUserVerifyingPlatformAuthenticatorAvailable=async()=>false;});
 await authenticator(context,page,true,'usb');
 await create(page);
 await upload(page,{name:'security-key.txt',mimeType:'text/plain',buffer:Buffer.from('public external passkey fixture')});
 await enable(page);await lock(page);await passkeyUnlock(page);
 await page.getByRole('button',{name:'Open security-key.txt',exact:true}).click();
 await expect(page.locator('dialog pre')).toHaveText('public external passkey fixture');
 await page.getByRole('button',{name:'Close',exact:true}).click();
 await lock(page);await passwordUnlock(page);
});

test('a platform authenticator without PRF leaves password unlock intact', async ({ page, context }) => {
    await authenticator(context, page, false);
    await create(page);
    await settings(page);
    await page.getByRole('button', { name: 'Enable passkey unlock', exact: true }).click();
    await expect(page.getByText('This browser or passkey provider cannot protect your unlock keys with PRF. Use your password.', { exact: true })).toBeVisible();
    expect(await page.evaluate(key => localStorage.getItem(key), await recordKey(page))).toBeNull();
    await lock(page);
    await expect(page.getByRole('button', { name: 'Unlock with passkey' })).toHaveCount(0);
    await passwordUnlock(page);
});

test('removing local passkey access survives reload and keeps password access', async ({ page, context }) => {
    await authenticator(context, page);
    await create(page);
    await enable(page);
    await page.getByRole('button', { name: 'Remove passkey unlock', exact: true }).click();
    expect(await page.evaluate(key => localStorage.getItem(key), await recordKey(page))).toBeNull();
    await page.reload();
    await expect(page.getByRole('button', { name: 'Unlock with passkey' })).toHaveCount(0);
    await passwordUnlock(page);
});

for (const field of ['wrapped', 'nonce', 'salt', 'credentialId', 'accountBinding', 'origin', 'rpId', 'version', 'malformed'] as const) {
    test('rejects a modified passkey record: ' + field, async ({ page, context }) => {
        await authenticator(context, page);
        await create(page);
        await enable(page);
        const key = await recordKey(page);
        await page.evaluate(({ key, field }) => {
            const record = JSON.parse(localStorage.getItem(key)!);
            if (field === 'malformed') { localStorage.setItem(key, '{'); return; }
            if (field === 'version') record.version = 2;
            else if (field === 'origin') record.origin = 'https://elsewhere.invalid';
            else if (field === 'rpId') record.rpId = 'elsewhere.invalid';
            else if (field === 'credentialId') record.credentialId = '!';
            else { const bytes = Uint8Array.from(atob(record[field]), ch => ch.charCodeAt(0)); bytes[0] ^= 1; record[field] = btoa(String.fromCharCode(...bytes)); }
            localStorage.setItem(key, JSON.stringify(record));
        }, { key, field });
        await lock(page);
        await page.getByRole('button', { name: 'Unlock with passkey', exact: true }).click();
        await expect(page.getByRole('status')).toContainText('Use your password');
        await expect(page.getByRole('heading', { name: 'Welcome back.', exact: true })).toBeVisible();
        await passwordUnlock(page);
    });
}

test('locking during pending setup aborts verification and never saves an envelope', async ({ page, context }) => {
    const { session, authenticatorId } = await authenticator(context, page);
    await create(page);
    await session.send('WebAuthn.setAutomaticPresenceSimulation', { authenticatorId, enabled: false });
    await settings(page);
    await page.getByRole('button', { name: 'Enable passkey unlock', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Cancel passkey setup', exact: true })).toBeVisible();
    await lock(page);
    await session.send('WebAuthn.setAutomaticPresenceSimulation', { authenticatorId, enabled: true });
    await expect(page.getByRole('button', { name: 'Unlock Klefki', exact: true })).toBeEnabled();
    expect(await page.evaluate(key => localStorage.getItem(key), await recordKey(page))).toBeNull();
    await passwordUnlock(page);
    await expect(page.getByRole('heading', { name: 'Documents', exact: true })).toBeVisible();
});

test('canceling setup restores settings without enabling passkey access', async ({ page, context }) => {
    const { session, authenticatorId } = await authenticator(context, page);
    await create(page);
    await session.send('WebAuthn.setAutomaticPresenceSimulation', { authenticatorId, enabled: false });
    await settings(page);
    await page.getByRole('button', { name: 'Enable passkey unlock', exact: true }).click();
    await page.getByRole('button', { name: 'Cancel passkey setup', exact: true }).click();
    await expect(page.getByText('Passkey verification was canceled or timed out. Try again, or use your password.', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Enable passkey unlock', exact: true })).toBeEnabled();
    expect(await page.evaluate(key => localStorage.getItem(key), await recordKey(page))).toBeNull();
});

test('a response without user verification cannot unlock the vault', async ({ page, context }) => {
    const { session, authenticatorId } = await authenticator(context, page);
    await create(page);
    await enable(page);
    await lock(page);
    await session.send('WebAuthn.setResponseOverrideBits', { authenticatorId, isBadUV: true });
    await page.getByRole('button', { name: 'Unlock with passkey', exact: true }).click();
    await expect(page.getByRole('status')).toContainText('Use your password');
    await passwordUnlock(page);
});

test('canceling passkey unlock allows immediate password fallback', async ({ page, context }) => {
    const { session, authenticatorId } = await authenticator(context, page);
    await create(page);
    await enable(page);
    await lock(page);
    await session.send('WebAuthn.setAutomaticPresenceSimulation', { authenticatorId, enabled: false });
    await page.getByRole('button', { name: 'Unlock with passkey', exact: true }).click();
    await page.getByRole('button', { name: 'Cancel passkey verification', exact: true }).click();
    await expect(page.getByRole('status')).toContainText('canceled or timed out');
    await expect(page.getByRole('button', { name: 'Unlock with passkey', exact: true })).toBeEnabled();
    await passwordUnlock(page);
});

test('reloading during passkey unlock cannot reopen the locked page', async ({ page, context }) => {
    const { session, authenticatorId } = await authenticator(context, page);
    await create(page);
    await enable(page);
    await lock(page);
    await session.send('WebAuthn.setAutomaticPresenceSimulation', { authenticatorId, enabled: false });
    await page.getByRole('button', { name: 'Unlock with passkey', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Cancel passkey verification', exact: true })).toBeVisible();
    await page.reload();
    await session.send('WebAuthn.setAutomaticPresenceSimulation', { authenticatorId, enabled: true });
    await expect(page.getByRole('button', { name: 'Unlock with passkey', exact: true })).toBeEnabled();
    await expect(page.getByRole('heading', { name: 'Welcome back.', exact: true })).toBeVisible();
    await passkeyUnlock(page);
});

test('removing an envelope during verification prevents the pending unlock', async ({ page, context }) => {
    await authenticator(context, page);
    await create(page);
    await enable(page);
    const key = await recordKey(page);
    await lock(page);
    await page.evaluate(() => {
        const get = navigator.credentials.get.bind(navigator.credentials);
        navigator.credentials.get = async options => {
            const result = await get(options);
            await new Promise<void>(resolve => { (window as unknown as { resumePasskey: () => void }).resumePasskey = resolve; });
            return result;
        };
    });
    await page.getByRole('button', { name: 'Unlock with passkey', exact: true }).click();
    await expect.poll(() => page.evaluate(() => typeof (window as unknown as { resumePasskey?: () => void }).resumePasskey)).toBe('function');
    await page.evaluate(key => {
        localStorage.removeItem(key);
        (window as unknown as { resumePasskey: () => void }).resumePasskey();
    }, key);
    await expect(page.getByRole('status')).toContainText('Use your password');
    await passwordUnlock(page);
});

test('passkey unlock respects another tab holding the store lock', async ({ page, context }) => {
    await authenticator(context, page);
    await create(page);
    await enable(page);
    await lock(page);
    const other = await context.newPage();
    await other.goto('http://localhost:5188');
    await passwordUnlock(other);
    await page.getByRole('button', { name: 'Unlock with passkey', exact: true }).click();
    await expect(page.getByRole('status')).toContainText('Close other tabs');
    await expect(page.getByRole('heading', { name: 'Welcome back.', exact: true })).toBeVisible();
    await lock(other);
    await passkeyUnlock(page);
});

test('IP-address origins disable setup with an explanation', async ({ page, context }) => {
    await authenticator(context, page);
    await create(page);
    await page.goto('http://127.0.0.1:5188');
    await page.getByLabel('Password', { exact: true }).fill(password);
    await page.getByLabel('Confirm password').fill(password);
    await page.getByRole('button', { name: 'Create Klefki', exact: true }).click();
    await page.getByRole('button', { name: 'I have saved it' }).click();
    await settings(page);
    await expect(page.getByRole('button', { name: 'Enable passkey unlock', exact: true })).toBeDisabled();
    await expect(page.getByText('Open Klefki on an HTTPS domain or localhost to use passkey unlock. IP-address URLs cannot use passkeys.', { exact: true })).toBeVisible();
});

test('missing WebAuthn keeps setup disabled and password usable', async ({ page }) => {
    await page.addInitScript(() => { Object.defineProperty(window,'PublicKeyCredential',{value:undefined}); });
    await create(page);
    await settings(page);
    await expect(page.getByRole('button', { name: 'Enable passkey unlock', exact: true })).toBeDisabled();
    await expect(page.getByText('Passkey unlock needs HTTPS (or localhost) and a browser with WebAuthn. Use your password.', { exact: true })).toBeVisible();
    await lock(page);
    await passwordUnlock(page);
});
