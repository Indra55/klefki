import { test, expect, type Page } from '@playwright/test';
import { backToLibrary, openSettings, documentAction, upload } from './ui.ts';
const password = 'public UI parity fixture password';
async function create(page: Page) {
    await page.goto('/');
    await page.getByLabel('Password', {exact:true}).fill(password);
    await page.getByLabel('Confirm password').fill(password);
    await page.getByRole('button', {name:'Create Klefki',exact:true}).click();
    await page.getByRole('button', {name:'I have saved it'}).click();
    await expect(page.locator('.library')).toBeVisible();
}
function pdfFixture() {
    const stream = 'BT /F1 24 Tf 40 120 Td (Klefki browser preview) Tj ET';
    const objects = [
        '<< /Type /Catalog /Pages 2 0 R >>',
        '<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>',
        '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 6 0 R >>',
        '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 6 0 R >>',
        '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
        '<< /Length ' + stream.length + ' >>\nstream\n' + stream + '\nendstream',
    ];
    let pdf = '%PDF-1.4\n'; const offsets = [0];
    objects.forEach((object,index) => { offsets.push(Buffer.byteLength(pdf)); pdf += (index + 1) + ' 0 obj\n' + object + '\nendobj\n'; });
    const start = Buffer.byteLength(pdf);
    pdf += 'xref\n0 7\n0000000000 65535 f \n' + offsets.slice(1).map(offset => String(offset).padStart(10,'0') + ' 00000 n \n').join('');
    pdf += 'trailer\n<< /Size 7 /Root 1 0 R >>\nstartxref\n' + start + '\n%%EOF\n';
    return Buffer.from(pdf);
}
test('desktop categories, custom tags, folder navigation, favorites and trash restoration', async ({page}) => {
    await create(page);
    const categories = page.getByRole('navigation', {name:'Document categories'});
    for (const name of ['All','Identity','Money','Legal','Health','Education','Work']) await expect(categories.getByRole('button',{name,exact:true})).toBeVisible();
    await page.getByRole('button',{name:'New folder',exact:true}).click();
    await page.getByLabel('Folder name').fill('Family');
    await page.getByRole('button',{name:'Create folder',exact:true}).click();
    await page.getByRole('button',{name:'Open Family',exact:true}).click();
    await upload(page,{name:'identity.txt',mimeType:'text/plain',buffer:Buffer.from('private identity fixture')});
    await documentAction(page,'identity.txt','Edit details & move');
    await page.getByLabel('Tags, separated by commas').fill('Identity, Personal');
    await page.getByLabel('Favorite',{exact:true}).check();
    await page.getByRole('button',{name:'Save changes',exact:true}).click();
    const crumbs = page.getByRole('navigation',{name:'Folder breadcrumb'});
    await crumbs.getByRole('button',{name:'All',exact:true}).click();
    await expect(page.getByRole('button',{name:'Open identity.txt',exact:true})).toHaveCount(0);
    await expect(page.getByRole('button',{name:'Open Family',exact:true})).toBeVisible();
    await page.getByRole('button',{name:'Open Family',exact:true}).click();
    await expect(crumbs.getByText('Family',{exact:true})).toHaveAttribute('aria-current','page');
    await expect(page.getByRole('button',{name:'Open identity.txt',exact:true})).toBeVisible();
    await categories.getByRole('button',{name:'Personal',exact:true}).click();
    await expect(page.locator('article')).toHaveCount(1);
    await documentAction(page,'identity.txt','Move to Trash');
    await expect(page.locator('article')).toHaveCount(0);
    await page.getByRole('button',{name:'View',exact:true}).click();
    await page.getByRole('menuitem',{name:'Trash',exact:true}).click();
    await expect(page.getByRole('button',{name:'Open identity.txt',exact:true})).toBeVisible();
    await page.getByRole('button',{name:'Restore',exact:true}).click();
    await page.getByRole('button',{name:'View',exact:true}).click();
    await page.getByRole('menuitem',{name:'Favorites',exact:true}).click();
    await expect(page.locator('article')).toHaveCount(1);
    await documentAction(page,'identity.txt','Edit details & move');
    await expect(page.getByLabel('Folder',{exact:true}).locator('option:checked')).toHaveText('Family');
    await page.getByRole('button',{name:'Cancel',exact:true}).click();
});
test('nested folders show direct children and breadcrumbs return to All at mobile width', async ({page}) => {
    await page.setViewportSize({width:390,height:844});
    await create(page);
    await upload(page,{name:'loose.txt',mimeType:'text/plain',buffer:Buffer.from('root document')});
    for (const name of ['Family','Hitanshu']) {
        await page.getByRole('button',{name:'New folder',exact:true}).click();
        await page.getByLabel('Folder name').fill(name);
        await page.getByRole('button',{name:'Create folder',exact:true}).click();
        await page.getByRole('button',{name:'Open '+name,exact:true}).click();
    }
    await upload(page,{name:'inside.txt',mimeType:'text/plain',buffer:Buffer.from('nested document')});
    await expect(page.getByRole('button',{name:'Open loose.txt',exact:true})).toHaveCount(0);
    const crumbs = page.getByRole('navigation',{name:'Folder breadcrumb'});
    await expect(crumbs.getByText('Hitanshu',{exact:true})).toHaveAttribute('aria-current','page');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({path:'/tmp/klefki-folder-breadcrumb.png'});
    await crumbs.getByRole('button',{name:'Family',exact:true}).click();
    await expect(page.getByRole('button',{name:'Open Hitanshu',exact:true})).toBeVisible();
    await expect(page.getByRole('button',{name:'Open inside.txt',exact:true})).toHaveCount(0);
    await crumbs.getByRole('button',{name:'All',exact:true}).click();
    await expect(page.getByRole('button',{name:'Open Family',exact:true})).toBeVisible();
    await expect(page.getByRole('button',{name:'Open loose.txt',exact:true})).toBeVisible();
    await expect(page.getByRole('button',{name:'Open Hitanshu',exact:true})).toHaveCount(0);
    await expect(page.getByRole('button',{name:'Open inside.txt',exact:true})).toHaveCount(0);
});
test('search finds documents across all folders and restores the current folder when cleared, including offline', async ({page,context}) => {
    await create(page);
    const add = async (name:string) => {
        await upload(page,{name,mimeType:'text/plain',buffer:Buffer.from('public global search fixture')});
        await expect(page.getByRole('button',{name:'Open '+name,exact:true})).toBeVisible();
    };
    const folder = async (name:string) => {
        await page.getByRole('button',{name:'New folder',exact:true}).click();
        await page.getByLabel('Folder name').fill(name);
        await page.getByRole('button',{name:'Create folder',exact:true}).click();
        await page.getByRole('button',{name:'Open '+name,exact:true}).click();
    };
    await add('invoice-trash.txt');
    await documentAction(page,'invoice-trash.txt','Move to Trash');
    await add('invoice-root.txt');
    await folder('Family');await folder('Nested');await add('invoice-deep.txt');
    const crumbs=page.getByRole('navigation',{name:'Folder breadcrumb'});
    await crumbs.getByRole('button',{name:'All',exact:true}).click();
    await folder('Other');await add('local.txt');
    const search=page.getByRole('searchbox',{name:'Search names and tags'});
    const matches=async()=>{
        await expect(page.getByRole('button',{name:'Open invoice-root.txt',exact:true})).toBeVisible();
        await expect(page.getByRole('button',{name:'Open invoice-deep.txt',exact:true})).toBeVisible();
        await expect(page.getByRole('button',{name:'Open invoice-trash.txt',exact:true})).toHaveCount(0);
        await expect(page.getByRole('button',{name:'Open local.txt',exact:true})).toHaveCount(0);
    };
    await page.getByRole('navigation',{name:'Document categories'}).getByRole('button',{name:'Money',exact:true}).click();
    await search.fill('  INVOICE  ');await matches();
    await page.getByRole('button',{name:'Clear search',exact:true}).click();
    await expect(page.getByRole('button',{name:'Open invoice-deep.txt',exact:true})).toHaveCount(0);
    await page.getByRole('navigation',{name:'Document categories'}).getByRole('button',{name:'All',exact:true}).click();
    await expect(page.getByRole('button',{name:'Open local.txt',exact:true})).toBeVisible();
    await search.fill('Nested');await page.getByRole('button',{name:'Open Nested',exact:true}).click();
    await expect(search).toHaveValue('');
    await expect(crumbs.getByText('Nested',{exact:true})).toHaveAttribute('aria-current','page');
    await expect(page.getByRole('button',{name:'Open invoice-deep.txt',exact:true})).toBeVisible();
    await page.getByRole('button',{name:'View',exact:true}).click();
    await page.getByRole('menuitem',{name:'Favorites',exact:true}).click();
    await search.fill('invoice');await matches();
    await page.evaluate(async()=>{await navigator.serviceWorker.ready;});
    await expect.poll(()=>page.evaluate(()=>!!navigator.serviceWorker.controller)).toBe(true);
    await context.setOffline(true);await page.reload();
    await page.getByLabel('Password',{exact:true}).fill(password);
    await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
    await page.getByRole('button',{name:'Open Other',exact:true}).click();
    await search.fill('invoice');await matches();
    await page.getByRole('button',{name:'Clear search',exact:true}).click();
    await expect(page.getByRole('button',{name:'Open local.txt',exact:true})).toBeVisible();
    await expect(page.getByRole('button',{name:'Open invoice-root.txt',exact:true})).toHaveCount(0);
    await crumbs.getByRole('button',{name:'All',exact:true}).click();
    await search.fill('invoice');await matches();
});
test('dedicated settings persist appearance, spacing, sort and idle lock across offline PWA reload', async ({page,context}) => {
    await page.setViewportSize({width:900,height:920});
    await create(page);
    await upload(page,{name:'paper.pdf',mimeType:'application/pdf',buffer:pdfFixture()});
    await upload(page,{name:'resume.txt',mimeType:'text/plain',buffer:Buffer.from('public resume fixture')});
    await page.screenshot({path:'/tmp/klefki-web-library.png'});
    await openSettings(page,'Appearance');
    await expect(page.getByRole('heading',{name:'Settings',exact:true})).toBeVisible();
    await expect(page.locator('.library')).toHaveCount(0);
    await page.screenshot({path:'/tmp/klefki-web-settings.png'});
    // Change multiple preferences before the asynchronous settings repaint.
    await page.locator('.settings-panel').evaluate(panel=>{
        for(const [key,value] of [['theme','light'],['density','compact'],['defaultSort','size']]){
            const select=panel.querySelector<HTMLSelectElement>('#preference-'+key)!;
            select.value=value;select.dispatchEvent(new Event('change',{bubbles:true}));
        }
    });
    await openSettings(page,'Privacy & lock');
    await page.getByLabel('Lock after inactivity',{exact:true}).selectOption('1');
    await backToLibrary(page);
    await page.evaluate(async () => { await navigator.serviceWorker.ready; });
    await expect.poll(() => page.evaluate(() => !!navigator.serviceWorker.controller)).toBe(true);
    await context.setOffline(true); await page.reload();
    await page.getByLabel('Password',{exact:true}).fill(password);
    await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
    await expect(page.locator('html')).toHaveAttribute('data-theme','light');
    await expect(page.locator('.library')).toHaveAttribute('data-density','compact');
    await expect(page.getByLabel('Sort documents')).toHaveValue('size');
    await openSettings(page,'Privacy & lock');
    await expect(page.getByLabel('Lock after inactivity',{exact:true})).toHaveValue('1');
    await page.clock.install(); await page.clock.fastForward(61000);
    await expect(page.getByRole('button',{name:'Unlock Klefki',exact:true})).toBeVisible();
    await expect(page.locator('article')).toHaveCount(0);
});
test('PDF pages and zoom work offline and lock clears the viewer', async ({page,context}) => {
    await create(page);
    await upload(page,{name:'paper.pdf',mimeType:'application/pdf',buffer:pdfFixture()});
    await page.evaluate(async () => { await navigator.serviceWorker.ready; });
    await expect.poll(() => page.evaluate(() => !!navigator.serviceWorker.controller)).toBe(true);
    await context.setOffline(true);
    await page.getByRole('button',{name:'Open paper.pdf',exact:true}).click();
    await expect(page.locator('dialog canvas')).toHaveAttribute('aria-label','Page 1 of paper.pdf');
    await expect(page.locator('dialog canvas')).toHaveJSProperty('width',375);
    await expect.poll(() => page.locator('canvas').evaluate(canvas => {
        const data = (canvas as HTMLCanvasElement).getContext('2d')!.getImageData(0,0,375,250).data;
        return data.some((value,index) => index % 4 !== 3 && value < 200);
    })).toBe(true);
    await page.getByRole('button',{name:'Next page',exact:true}).click();
    await expect(page.locator('dialog canvas')).toHaveAttribute('aria-label','Page 2 of paper.pdf');
    await page.getByRole('button',{name:'Zoom in',exact:true}).click();
    await expect(page.locator('dialog canvas')).toHaveJSProperty('width',468);
    await page.keyboard.press('Control+Shift+L');
    await expect(page.locator('dialog')).toHaveCount(0);
    await expect(page.getByRole('button',{name:'Unlock Klefki',exact:true})).toBeVisible();
});
test('mobile layout keeps library and settings within the viewport', async ({browser}) => {
    const context = await browser.newContext({baseURL:'http://127.0.0.1:5188',viewport:{width:390,height:844},isMobile:true,hasTouch:true});
    const page = await context.newPage();
    try {
        await create(page);
        await upload(page,{name:'a long document name that still fits.pdf',mimeType:'application/pdf',buffer:pdfFixture()});
        await page.screenshot({path:'/tmp/klefki-pwa-mobile.png'});
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        await openSettings(page,'Appearance');
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        await expect(page.getByLabel('Color theme',{exact:true})).toBeVisible();
        await openSettings(page,'Offline & backup');
        for(const width of [320,390,600]){
            await page.setViewportSize({width,height:844});
            expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
            await expect(page.locator('[data-storage-protection]')).not.toBeEmpty();
            await expect(page.locator('[data-backup-status]')).toContainText('No backup folder selected.');
        }
        await page.screenshot({path:'/tmp/klefki-pwa-offline-settings.png',fullPage:true});
    } finally { await context.close(); }
});

test('a PWA update locks and reloads while preserving the encrypted account and local documents', async ({page}) => {
    const {readFileSync,writeFileSync} = await import('node:fs');
    const workerPath = 'dist/sw.js', original = readFileSync(workerPath,'utf8');
    await create(page);
    await upload(page,{name:'update-safe.txt',mimeType:'text/plain',buffer:Buffer.from('public update persistence fixture')});
    await page.evaluate(async () => { await navigator.serviceWorker.ready; });
    await expect.poll(() => page.evaluate(() => !!navigator.serviceWorker.controller)).toBe(true);
    const account = await page.evaluate(() => localStorage.getItem('klefki-account'));
    try {
        writeFileSync(workerPath,original + '\n// public update fixture ' + Date.now() + '\n');
        await page.evaluate(async () => { await (await navigator.serviceWorker.ready).update(); });
        await expect(page.getByRole('button',{name:'Update and reopen',exact:true})).toBeVisible();
        await openSettings(page,'Appearance');
        await expect(page.getByRole('button',{name:'Update and reopen',exact:true})).toBeVisible();
        await Promise.all([
            page.waitForEvent('load'),
            page.getByRole('button',{name:'Update and reopen',exact:true}).click(),
        ]);
        await expect(page.getByRole('button',{name:'Unlock Klefki',exact:true})).toBeVisible();
        await expect(page.locator('article')).toHaveCount(0);
        expect(await page.evaluate(() => localStorage.getItem('klefki-account'))).toBe(account);
        await page.getByLabel('Password',{exact:true}).fill(password);
        await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
        await expect(page.getByRole('button',{name:'Open update-safe.txt',exact:true})).toBeVisible();
    } finally { writeFileSync(workerPath,original); }
});
