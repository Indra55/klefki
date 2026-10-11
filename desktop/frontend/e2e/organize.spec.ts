import { test, expect } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    const listeners: Record<string, Function> = {};
    const state = { config: { accountPath: "fixture.v1", storePath: "fixture.db", server: "", preferences: { theme: "dark", idleMinutes: 5 } }, unlocked: true, generation: 1, accountID: "fixture", recoveryOnly: false, sync: "Local only", pending: 0, lastSync: "", semantic: { enabled: true, thresholds: { cutoff: .5, margin: .01 } } };
    const items = ["invoice.txt", "jazz concert.txt", "jazz rehearsal.txt"].map((name, i) => ({ object: String(i), revision: String(i), name, type: "file", parent: null, tags: i === 0 ? ["Personal"] : [], favorite: false, size: 120, conflict: false, trashed: false, trashRoot: false }));
    const bridge = {
      State: async () => structuredClone(state), List: async () => ({ generation: state.generation, items: structuredClone(items) }), Touch: async () => {},
      SuggestOrganization: async (custom: string[]) => {
        (window as any).__customTags = custom;
        return { generation: state.generation, skipped: 0, tags: ["Money", "Jazz Music"], suggestions: items.map((item, i) => ({ revision: item.revision, name: item.name, tags: item.tags, tag: i === 0 ? "Money" : "Jazz Music", new: i > 0, basis: i === 0 ? "matching tag" : "new content group", score: .8 })) };
      },
      ApplyOrganization: async (generation: number, changes: { revision: string; tag: string }[]) => {
        if (generation !== state.generation) throw new Error("STALE");
        (window as any).__appliedTags = changes;
        for (const change of changes) items.find(item => item.revision === change.revision)!.tags.push(change.tag);
        return changes.length;
      },
      Lock: async () => { state.unlocked = false; state.generation++; listeners["klefki:event"]?.({ generation: state.generation, kind: "locked" }); },
    };
    (window as any).runtime = { EventsOn: (name: string, cb: Function) => { listeners[name] = cb; return () => delete listeners[name]; } };
    (window as any).go = { main: { App: bridge } };
    (window as any).__lockFixture = bridge.Lock;
  });
});

for (const width of [390, 1200]) {
  test(`reviews new tags and filters organized documents at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 820 });
    await page.goto("/");
    await page.getByRole("button", { name: "Organize files", exact: true }).click();
    await page.getByLabel("Custom tags to match", { exact: true }).fill("Music");
    await page.getByRole("button", { name: "Find tags", exact: true }).click();
    await expect(page.getByLabel("Tag for Jazz Music")).toHaveValue("Jazz Music");
    expect(await page.evaluate(() => (window as any).__appliedTags)).toBeUndefined();
    await page.getByLabel("Tag for Jazz Music").fill("Music");
    await page.getByRole("checkbox", { name: /^invoice\.txt/ }).uncheck();
    await page.screenshot({ path: `test-results/organize-${width}.png`, fullPage: true });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
    await page.getByRole("button", { name: "Apply tags to 2 files" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByText("Added tags to 2 files.", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Music", exact: true }).click();
    await expect(page.getByText("jazz concert.txt", { exact: true })).toBeVisible();
    await expect(page.getByText("jazz rehearsal.txt", { exact: true })).toBeVisible();
    await expect(page.getByText("invoice.txt", { exact: true })).toHaveCount(0);
    expect(await page.evaluate(() => (window as any).__appliedTags)).toEqual([{ revision: "1", tag: "Music" }, { revision: "2", tag: "Music" }]);
  });
}

test("locking clears an open organization review", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Organize files", exact: true }).click();
  await page.getByRole("button", { name: "Find tags", exact: true }).click();
  await expect(page.getByLabel("Tag for Jazz Music")).toBeVisible();
  await page.evaluate(() => (window as any).__lockFixture());
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.getByText("Unlock Klefki.", { exact: true })).toBeVisible();
  await expect(page.getByText("jazz concert.txt", { exact: true })).toHaveCount(0);
});
