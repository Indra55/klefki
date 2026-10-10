import { test, expect, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync, readFileSync, readdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
const origin = "http://127.0.0.1:5188",
  password = "public M5 browser fixture password";
let dir: string;
function bridge(mode: string) {
  return execFileSync(
    "go",
    ["run", "-tags", "onnx", "./tests/bridge", mode, dir, origin],
    { stdio: "pipe", timeout: 90000 },
  ).toString();
}
async function unlock(page: Page) {
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Unlock Klefki", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Documents", exact: true }),
  ).toBeVisible();
}
async function connect(page: Page) {
  await page.goto("/");
  await page
    .getByLabel("Open an encrypted account file")
    .setInputFiles(join(dir, "account.klefki"));
  await unlock(page);
  await page.getByRole("button", { name: "Sync now", exact: true }).click();
  await expect(page.locator("#status")).toContainText("Synced.");
}
async function enable(page: Page) {
  await page.getByText("Klefki settings", { exact: true }).click();
  await page
    .getByLabel("Semantic search and suggestions on this device", {
      exact: true,
    })
    .check();
  await expect(
    page.getByLabel("Find your documents", { exact: true }),
  ).toBeVisible({ timeout: 90000 });
  await page.evaluate(async () => {
    await navigator.serviceWorker.ready;
  });
  await expect
    .poll(() => page.evaluate(() => !!navigator.serviceWorker.controller))
    .toBe(true);
}
test.beforeEach(() => {
  test.skip(
    !process.env.KLEFKI_MODEL_DIR,
    "Set KLEFKI_MODEL_DIR to require real M8 browser acceptance",
  );
  dir = mkdtempSync(join(tmpdir(), "klefki-m8-web-"));
  bridge("seed");
  bridge("index-semantic");
});
test.afterEach(() => {
  if (dir) rmSync(dir, { recursive: true, force: true });
});
test("M8 is off by default; real browser model matches Go and works offline after reload", async ({
  page,
  context,
}) => {
  const modelRequests: string[] = [];
  page.on("request", (r) => {
    if (r.url().includes("/models/")) modelRequests.push(r.url());
  });
  await connect(page);
  expect(modelRequests).toHaveLength(0);
  await expect(page.getByLabel("Search names and tags")).toBeVisible();
  await expect(page.locator("article")).toHaveCount(3);
  await enable(page);
  const worker = readdirSync("dist/assets").find((n) =>
    /^semantic-worker-.*\.js$/.test(n),
  )!;
  const parity = JSON.parse(readFileSync("tests/m8-parity.json", "utf8"));
  const result = await page.evaluate(
    ({ worker, text }) =>
      new Promise<any>((resolve, reject) => {
        const w = new Worker("/assets/" + worker, { type: "module" });
        w.onerror = reject;
        w.onmessage = (e) => {
          w.terminate();
          e.data.error ? reject(Error(e.data.error)) : resolve(e.data.result);
        };
        w.postMessage({ id: 1, text });
      }),
    { worker, text: parity.text },
  );
  expect(result.tokens).toEqual(parity.tokens);
  expect(result.fingerprint).toBe(parity.fingerprint);
  expect(
    Math.max(
      ...result.vector.map((v: number, i: number) =>
        Math.abs(v - parity.vector[i]),
      ),
    ),
  ).toBeLessThan(0.0001);
  await context.setOffline(true);
  await page.reload();
  await unlock(page);
  await page.getByLabel("Find your documents").fill("original text");
  await expect(
    page.getByText("Semantic + keyword search", { exact: true }),
  ).toBeVisible({ timeout: 90000 });
  await expect(
    page
      .locator("article")
      .filter({ has: page.getByText("note.txt", { exact: true }) }),
  ).toBeVisible();
  await page.getByText("Klefki settings", { exact: true }).click();
  await page
    .getByLabel("Semantic search and suggestions on this device", {
      exact: true,
    })
    .uncheck();
  await expect(page.getByLabel("Search names and tags")).toBeVisible();
  await page.getByRole("button", { name: "Lock", exact: true }).click();
  await expect(page.locator("article")).toHaveCount(0);
});
test("web category corrections sync to Go and internal sidecars stay hidden", async ({
  page,
}) => {
  await connect(page);
  const account = (await page.locator(".account-id").textContent())!.replace("Account ID: ", "");
  await page.evaluate((account) => {
    localStorage.setItem("klefki-semantic-" + account, JSON.stringify({ enabled: true, cutoff: .2, margin: 0, modelFingerprint: "legacy-minilm" }));
  }, account);
  await page.reload();
  await unlock(page);
  await page.getByText("Klefki settings", { exact: true }).click();
  await expect(page.getByLabel("Semantic search and suggestions on this device", { exact: true })).toBeChecked();
  await expect(page.getByLabel("Classification similarity cutoff")).toHaveValue("0.5");
  await expect(page.getByLabel("Winning category margin")).toHaveValue("0.01");
  await page.getByRole("button", { name: "Install BGE-small model", exact: true }).click();
  await expect(page.getByRole("button", { name: "Cancel setup", exact: true })).toBeHidden({ timeout: 90000 });
  await expect(page.getByLabel("Find your documents", { exact: true })).toBeVisible({ timeout: 90000 });
  const row = page
    .locator("article")
    .filter({ has: page.getByText("note.txt", { exact: true }) });
  await row.getByRole("button", { name: "Suggestions", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Local suggestions" }),
  ).toBeVisible();
  await page.getByLabel("Suggested category").selectOption("Money");
  await page
    .getByRole("button", { name: "Confirm category and fields" })
    .click();
  await expect(page.locator("article")).toHaveCount(3);
  await page.getByRole("button", { name: "Sync now", exact: true }).click();
  await expect(page.locator("#status")).toContainText("Synced.");
  expect(bridge("verify-correction")).toContain("PASS correction");
  await page.getByLabel("Document category").selectOption("Money");
  await expect(page.locator("article")).toHaveCount(1);
});

test("long questions exclude incidental keyword matches and retain exact identifiers", async ({ page }) => {
  expect(bridge("relevance")).toContain("PASS relevance fixture");
  await connect(page);
  await expect(page.locator("article")).toHaveCount(7);
  await enable(page);
  const query = page.getByLabel("Find your documents");
  await query.fill("Which record combines a consumption history visualization with instructions for settling an outstanding balance?");
  await expect(page.getByText("Semantic + keyword search", { exact: true })).toBeVisible({ timeout: 90000 });
  await expect(page.locator("article")).toHaveCount(1);
  await expect(page.locator("article")).toContainText("utility.txt");
  await query.fill("Which document contains the identifier 731924?");
  await expect(page.getByText("Semantic + keyword search", { exact: true })).toBeVisible();
  await expect(page.locator("article")).toHaveCount(1);
  await expect(page.locator("article")).toContainText("utility.txt");
  await query.fill("Which record describes the orbital mechanics of a lunar spacecraft?");
  await expect(page.getByText("Semantic + keyword search", { exact: true })).toBeVisible();
  await expect(page.locator("article")).toHaveCount(0);
});
