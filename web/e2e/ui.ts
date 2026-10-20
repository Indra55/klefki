import { expect, type Page } from '@playwright/test';

export async function backToLibrary(page: Page) {
    const back = page.getByRole('button', {name: 'Back to library', exact: true});
    if (await back.isVisible()) {await back.click();await expect(page.getByRole('heading',{name:'Documents',exact:true})).toBeVisible();}
}
export async function openSettings(page: Page, section: string) {
    await page.getByRole('button', {name: 'Settings', exact: true}).click();
    await page.getByRole('navigation', {name: 'Settings sections'}).getByRole('button', {name: section, exact: true}).click();
}
export async function syncNow(page: Page) {
    await backToLibrary(page);
    await page.getByRole('button', {name: 'Sync status', exact: true}).click();
    const panel=page.getByRole('dialog', {name: 'Sync activity'});
    await panel.getByRole('button', {name: 'Refresh sync', exact: true}).click();
    await panel.getByRole('button', {name: 'Close', exact: true}).click();
}
export async function documentAction(page: Page, name: string, action: string) {
    await backToLibrary(page);
    const row = page.locator('article').filter({has: page.getByText(name, {exact: true})});
    await row.getByRole('button', {name: 'Actions for ' + name, exact: true}).click();
    await row.getByRole('menuitem', {name: action, exact: true}).click();
}
export async function upload(page: Page, file: {name: string; mimeType: string; buffer: Buffer}) {
    await backToLibrary(page);
    await page.getByLabel('Add document').setInputFiles(file);
}
