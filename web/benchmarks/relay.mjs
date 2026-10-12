import {chromium} from 'playwright';
import {spawn, execFileSync} from 'node:child_process';
import {createInterface} from 'node:readline';
import {parseArgs} from 'node:util';
import {createHash, randomBytes} from 'node:crypto';
import {mkdirSync, existsSync, readdirSync, readFileSync, writeFileSync, linkSync} from 'node:fs';
import {dirname, join, resolve, parse} from 'node:path';
import {fileURLToPath} from 'node:url';
import {once} from 'node:events';
import * as c from '../src/crypto.ts';
import {fixtures} from './fixtures.mjs';

const web = resolve(dirname(fileURLToPath(import.meta.url)), '..'), repo = resolve(web, '..');
const {values: options} = parseArgs({options: {
  local: {type: 'boolean'}, server: {type: 'string'}, out: {type: 'string'},
  quick: {type: 'boolean'}, history: {type: 'string', default: '59'},
  'rate-burst': {type: 'string', default: '12000'}, 'skip-web-build': {type: 'boolean'},
  'latency-probe': {type: 'boolean'}, help: {type: 'boolean'},
}});
if (options.help) {
  console.log(`Run from the repository root:
  npm --prefix web run benchmark:local
  npm --prefix web run benchmark -- --server https://YOUR-RELAY

Options:
  --quick             Two sizes in both directions, duplication, and no-edit syncs
  --history N         Synthetic initial folder revisions (default: 59)
  --out DIRECTORY     Fresh output directory (default: .benchmarks/<run>)
  --latency-probe     Five Go no-edit samples each with 0, 100, and 300 ms/request
  --rate-burst N      Local-only burst budget (default: 12000; production: 120)
  --skip-web-build    Reuse web/dist for a local run

Every run creates its own synthetic account; no personal account file is needed.`);
  process.exit(0);
}
if (!!options.local === !!options.server) throw Error('Choose --local or --server URL.');
const history = Number(options.history), rateBurst = Number(options['rate-burst']);
if (!Number.isInteger(history) || history < 0 || history > 4096) throw Error('--history must be between 0 and 4096');
if (!Number.isInteger(rateBurst) || rateBurst < 1) throw Error('--rate-burst must be a positive integer');
if (options.server) {
  const u = new URL(options.server);
  if (u.username || u.password || u.search || u.hash || !['', '/'].includes(u.pathname) ||
      !(u.protocol === 'https:' || u.protocol === 'http:' && ['127.0.0.1', '[::1]'].includes(u.hostname))) {
    throw Error('Use an HTTPS origin or a literal loopback HTTP origin.');
  }
}
const run = new Date().toISOString().replace(/[:.]/g, '-') + '-' + randomBytes(3).toString('hex');
const dir = options.out ? resolve(options.out) : join(repo, '.benchmarks', run);
if (existsSync(dir) && readdirSync(dir).length) throw Error('Output directory must be new or empty.');
mkdirSync(dir, {recursive: true, mode: 0o700});
mkdirSync(join(dir, 'fixtures'), {mode: 0o700});
const controller = new AbortController(), sleep = ms => new Promise(r => setTimeout(r, ms));
for (const signal of ['SIGINT', 'SIGTERM']) process.once(signal, () => controller.abort(Error('Benchmark interrupted')));
const result = {
  started: new Date().toISOString(), run, mode: options.local ? 'local' : 'hosted',
  quick: !!options.quick, history, local_rate_burst: options.local ? rateBurst : null,
  browser_waits: {after_click_ms: 200, polling_ms: 100, settling_ms: 150},
  baseline: 'Fresh synthetic account with folder history and four offline holding peers',
  rows: [], checks: [], latency_probe: [], cleanup: {removed_devices: [], revoked_sessions: [], errors: []},
};
const persist = () => writeFileSync(join(dir, 'results.json'), JSON.stringify(result, null, 2) + '\n');
try {
  result.revision = execFileSync('git', ['rev-parse', 'HEAD'], {cwd: repo, encoding: 'utf8'}).trim();
  const diff = execFileSync('git', ['diff', 'HEAD'], {cwd: repo});
  result.source_diff_sha256 = createHash('sha256').update(diff).digest('hex');
  result.working_tree = execFileSync('git', ['status', '--short'], {cwd: repo, encoding: 'utf8'}).trim();
  writeFileSync(join(dir, 'source.patch'), diff);
} catch {}
const sourceFiles = [];
function sourceManifest(path) {
  for (const entry of readdirSync(join(repo, path), {withFileTypes: true})) {
    const name = join(path, entry.name);
    if (entry.isDirectory()) sourceManifest(name);
    else if (/\.(go|ts|mjs)$/.test(name)) sourceFiles.push({path: name, sha256: createHash('sha256').update(readFileSync(join(repo, name))).digest('hex')});
  }
}
for (const path of ['core', 'server', 'web/src', 'web/benchmarks', 'ui']) sourceManifest(path);
writeFileSync(join(dir, 'source-manifest.json'), JSON.stringify(sourceFiles, null, 2)+'\n');
result.source_manifest_sha256 = createHash('sha256').update(JSON.stringify(sourceFiles)).digest('hex');

let base = options.server?.replace(/\/$/, ''), server, worker, context, page, admin;
const devices = [], sessions = new Set(), prefix = 'relaybench-' + run + '-';
let account, password, active = false, logs = [], pending = new Map();

async function command(program, args, cwd = repo) {
  const child = spawn(program, args, {cwd, stdio: ['ignore', 'inherit', 'inherit'], signal: controller.signal});
  const [code] = await once(child, 'exit');
  if (code !== 0) throw Error(`${program} exited with status ${code}`);
}
function rpcWorker(child) {
  const queue = [];
  let closed;
  child.on('error', fail);
  child.on('exit', code => fail(Error('Go benchmark client exited with status ' + code)));
  function fail(e) { closed = e; for (const call of queue.splice(0)) call.reject(e); }
  createInterface({input: child.stdout}).on('line', line => {
    const call = queue.shift();
    if (!call) return;
    try { const reply = JSON.parse(line); reply.error ? call.reject(Error(reply.error)) : call.resolve(reply); }
    catch (e) { call.reject(e); }
  });
  return request => new Promise((resolve, reject) => {
    if (closed) { reject(closed); return; }
    queue.push({resolve, reject}); child.stdin.write(JSON.stringify(request) + '\n');
  });
}
let rpc;
async function api(method, path, body, cleanup = false) {
  for (let attempt = 0; ; attempt++) {
    const signal = cleanup ? AbortSignal.timeout(10000) : AbortSignal.any([controller.signal, AbortSignal.timeout(30000)]);
    const response = await fetch(base + path, {
      method, redirect: 'error', signal,
      headers: {...(admin ? {Authorization: 'Bearer ' + admin.token} : {}), ...(body ? {'Content-Type': 'application/json'} : {})},
      body: body ? JSON.stringify(body) : undefined,
    });
    if (response.status === 429 && attempt < 3) {
      const seconds = Number(response.headers.get('Retry-After')) || 60;
      await response.body?.cancel();
      console.log(`Setup request throttled; retrying in ${Math.min(120, seconds)} seconds.`);
      const retrySignal = cleanup ? AbortSignal.timeout(125000) : controller.signal;
      retrySignal.throwIfAborted();
      await new Promise((resolve, reject) => {
        const abort = () => { clearTimeout(timer); reject(retrySignal.reason); };
        const timer = setTimeout(() => { retrySignal.removeEventListener('abort', abort); resolve(); }, Math.min(120, seconds) * 1000);
        retrySignal.addEventListener('abort', abort, {once: true});
      });
      continue;
    }
    if (!response.ok) { await response.body?.cancel(); throw Error(`${method} ${path}: HTTP ${response.status}`); }
    const text = await response.text();
    return text ? JSON.parse(text) : {};
  }
}
async function library() {
  const back = page.getByRole('button', {name: 'Back to library', exact: true});
  if (await back.isVisible()) await back.click();
  const dialog = page.getByRole('dialog', {name: 'Sync activity'});
  if (await dialog.isVisible()) await page.keyboard.press('Escape');
}
async function syncUI() {
  controller.signal.throwIfAborted();
  await library();
  await page.getByRole('button', {name: 'Sync status', exact: true}).click();
  await page.getByRole('dialog', {name: 'Sync activity'}).getByRole('button', {name: 'Sync now', exact: true}).click();
  await sleep(200);
  const deadline = Date.now() + 12 * 60000;
  while (Date.now() < deadline) {
    controller.signal.throwIfAborted();
    if (await page.getByRole('button', {name: 'Sync status', exact: true}).getAttribute('data-state') !== 'running') {
      await sleep(150);
      const status = await page.locator('#status').textContent();
      if (/Could not complete|expired|storage is full|Waiting for another/.test(status || '')) throw Error(status);
      return status;
    }
    await sleep(100);
  }
  throw Error('Browser sync timed out');
}
function startBrowser() { active = true; logs = []; return performance.now(); }
function browserStats(start) {
  active = false;
  const foreground = logs.filter(x => !['/v1/sync/status', '/v1/sync/presence'].includes(x.path) && x.start >= start && x.end !== undefined);
  return {
    seconds: (performance.now() - start) / 1000, requests: logs.length,
    chunks_up: logs.filter(x => x.method === 'PUT' && /\/chunks\/\d+$/.test(x.path)).length,
    chunks_down: logs.filter(x => x.method === 'GET' && /\/chunks\/\d+$/.test(x.path)).length,
    foreground_span_seconds: foreground.length ? (Math.max(...foreground.map(x => x.end)) - Math.min(...foreground.map(x => x.start))) / 1000 : null,
    http_errors: logs.filter(x => x.status >= 400 || x.failed).map(x => ({path: x.path, status: x.status, failed: x.failed})),
    trace: logs.map(x => ({method: x.method, path: x.path, start_ms: x.start - start, end_ms: x.end === undefined ? null : x.end - start, status: x.status, failed: x.failed})),
  };
}
async function select(f, name) {
  await library();
  if (f.size > 50 * 1024 * 1024) {
    const alias = join(dir, 'fixtures', name);
    if (!existsSync(alias)) linkSync(f.path, alias);
    await page.getByLabel('Add document').setInputFiles(alias);
  } else {
    await page.getByLabel('Add document').setInputFiles({name, mimeType: f.mime, buffer: readFileSync(f.path)});
  }
  await page.locator('article').filter({has: page.getByText(name, {exact: true})}).first().waitFor();
}
async function exportHash(name) {
  await library();
  const row = page.locator('article').filter({has: page.getByText(name, {exact: true})}).first();
  await row.getByRole('button', {name: 'Actions for ' + name, exact: true}).click();
  const download = page.waitForEvent('download');
  await page.getByRole('menuitem', {name: 'Export', exact: true}).click();
  const stream = await (await download).createReadStream(), hash = createHash('sha256');
  for await (const bytes of stream) hash.update(bytes);
  return hash.digest('hex');
}
async function transfer(f, name, direction) {
  const row = {direction, name, type: f.mime, size: f.size, expected_sha256: f.sha256};
  if (direction === 'browser-to-go') {
    const save = performance.now();
    await select(f, name); row.local_save_seconds = (performance.now() - save) / 1000;
    const start = startBrowser(); row.status = await syncUI(); row.browser_sync = browserStats(start);
    row.destination = await rpc({Op: 'sync'});
    row.matches = (await rpc({Op: 'verify', Name: name})).matches;
    row.integrity_ok = row.matches.length === 1 && row.matches[0].sha256 === f.sha256;
  } else {
    row.local_save = await rpc({Op: 'put', Path: f.path, Name: name});
    row.source = await rpc({Op: 'sync'});
    const start = startBrowser(); row.status = await syncUI(); row.browser_sync = browserStats(start);
    await library();
    row.received_count = await page.locator('article').filter({has: page.getByText(name, {exact: true})}).count();
    row.received_sha256 = await exportHash(name);
    row.integrity_ok = row.received_count === 1 && row.received_sha256 === f.sha256;
  }
  result.rows.push(row); persist();
  console.log(`${direction} ${f.name}: browser ${row.browser_sync.seconds.toFixed(3)}s, Go ${(row.source || row.destination).seconds.toFixed(3)}s, SHA-256 ${row.integrity_ok ? 'OK' : 'FAILED'}`);
  if (!row.integrity_ok) throw Error('Integrity or duplicate check failed: ' + name);
}

try {
  await command('go', ['build', '-o', join(dir, 'client'), './web/benchmarks/client']);
  if (options.local) {
    if (!options['skip-web-build']) await command('npm', ['run', 'build'], web);
    await command('go', ['build', '-o', join(dir, 'server'), './web/benchmarks/server']);
    server = spawn(join(dir, 'server'), ['-data', join(dir, 'server-data'), '-web', join(web, 'dist'), '-rate-burst', String(rateBurst)], {stdio: ['ignore', 'pipe', 'inherit']});
    base = await new Promise((resolve, reject) => {
      server.once('error', reject); server.once('exit', code => reject(Error('Local server exited: ' + code)));
      createInterface({input: server.stdout}).once('line', line => { try { resolve(JSON.parse(line).url); } catch (e) { reject(e); } });
    });
  }
  result.base = base;
  console.log(`Benchmark relay: ${base}\nOutput: ${dir}`);
  await c.ready;
  password = randomBytes(32).toString('hex');
  const created = c.createAccount(password); account = created.account;
  c.wipe(created.master);
  const keys = c.unlock(account, password);
  try { admin = await api('POST', '/v1/signup', {...account, credential: c.base64(keys.credential)}); }
  finally { c.wipe(keys.master); c.wipe(keys.credential); }
  sessions.add(admin.device);
  writeFileSync(join(dir, 'account.v1'), c.accountBytes(account), {mode: 0o600});
  await api('PUT', '/v1/sync/settings', {mode: 'temporary-relay'});
  for (let i = 0; i < 4; i++) {
    const device = randomBytes(16).toString('hex');
    await api('POST', '/v1/sync/relay/device', {device, known: [], sequence: 0}); devices.push(device);
  }
  worker = spawn(join(dir, 'client'), ['-account', join(dir, 'account.v1'), '-store', join(dir, 'desktop.db'), '-server', base], {stdio: ['pipe', 'pipe', 'inherit']});
  controller.signal.addEventListener('abort', () => worker.kill('SIGTERM'), {once: true});
  rpc = rpcWorker(worker);
  const initialized = await rpc({Op: 'init', Password: password});
  devices.push(initialized.device); sessions.add(initialized.session_device);
  await rpc({Op: 'seed', History: history});
  result.seed = await rpc({Op: 'sync'});
  result.initial_relay = await api('GET', '/v1/sync/settings');
  if (result.initial_relay.queued !== history) throw Error('Unexpected initial history count');
  const files = fixtures(join(dir, 'fixtures'), options.quick);
  result.fixtures = files.map(({path, ...f}) => f);
  context = await chromium.launchPersistentContext(join(dir, 'browser-profile'), {headless: true, acceptDownloads: true});
  page = context.pages()[0] || await context.newPage(); page.setDefaultTimeout(180000);
  page.on('request', request => {
    if (active && new URL(request.url()).pathname.startsWith('/v1/')) {
      const x = {method: request.method(), path: new URL(request.url()).pathname, start: performance.now()};
      pending.set(request, x); logs.push(x);
    }
  });
  page.on('response', async response => {
    const x = pending.get(response.request()); if (x) x.status = response.status();
    if (new URL(response.url()).pathname === '/v1/login' && response.ok()) {
      try { sessions.add((await response.json()).device); } catch {}
    }
  });
  page.on('requestfinished', request => { const x = pending.get(request); if (x) { x.end = performance.now(); pending.delete(request); } });
  page.on('requestfailed', request => { const x = pending.get(request); if (x) { x.end = performance.now(); x.failed = request.failure()?.errorText; pending.delete(request); } });
  await page.goto(base, {waitUntil: 'domcontentloaded'});
  result.browser_assets = await page.locator('script[src]').evaluateAll(es => es.map(x => x.getAttribute('src')));
  await page.getByLabel('Open an encrypted account file').setInputFiles(join(dir, 'account.v1'));
  await page.getByLabel('Password', {exact: true}).fill(password);
  await page.getByRole('button', {name: 'Unlock Klefki', exact: true}).click();
  password = undefined;
  await page.getByRole('heading', {name: 'Documents', exact: true}).waitFor();
  await syncUI(); await rpc({Op: 'sync'}); await syncUI();
  const names = await page.evaluate(async () => {
    const databases = await indexedDB.databases();
    const db = await new Promise((resolve, reject) => {
      const request = indexedDB.open(databases.find(x => x.name.startsWith('klefki-v1-')).name);
      request.onsuccess = () => resolve(request.result); request.onerror = () => reject(request.error);
    });
    const device = await new Promise(resolve => {
      const request = db.transaction('state').objectStore('state').get('relay-device'); request.onsuccess = () => resolve(request.result);
    });
    db.close(); return device;
  });
  devices.push(names); persist();
  const sweep = options.quick ? files.filter(f => f.name !== 'archive.zip') : files;
  for (const f of sweep) {
    const parsed = parse(f.name), runs = !options.quick && f.size <= 1024 * 1024 ? 2 : 1;
    for (let i = 1; i <= runs; i++) await transfer(f, prefix + 'browser-' + parsed.name + '-r' + i + parsed.ext, 'browser-to-go');
  }
  for (const f of sweep.filter(f => f.name !== 'binary-64MiB.bin')) await transfer(f, prefix + 'go-' + f.name, 'go-to-browser');
  const duplicate = files.find(f => f.name === 'archive.zip'), duplicateName = prefix + 'duplicate.zip';
  await transfer(duplicate, duplicateName, 'browser-to-go');
  const duplicateStart = startBrowser();
  await select(duplicate, duplicateName);
  const duplicateDeadline = Date.now() + 180000;
  while (await page.locator('article').filter({has: page.getByText(duplicateName, {exact: true})}).count() !== 2) {
    if (Date.now() > duplicateDeadline) throw Error('Second local import did not appear');
    controller.signal.throwIfAborted(); await sleep(20);
  }
  await syncUI(); const repeatedUpload = browserStats(duplicateStart);
  await rpc({Op: 'sync'});
  const matches = (await rpc({Op: 'verify', Name: duplicateName})).matches;
  result.checks.push({check: 'identical file imported twice', second_upload: repeatedUpload, matches});
  if (matches.length !== 2 || matches.some(x => x.sha256 !== duplicate.sha256) || !repeatedUpload.chunks_up) throw Error('Repeated import verification failed');
  const beforeIdle = await rpc({Op: 'summary'});
  for (let i = 0; i < 3; i++) {
    const start = startBrowser(); await syncUI(); const browser = browserStats(start), go = await rpc({Op: 'sync'});
    result.checks.push({check: 'no-edit sync ' + (i + 1), browser, go});
    if (browser.chunks_up || browser.chunks_down || go.metrics.chunks_up || go.metrics.chunks_down) throw Error('No-edit sync transferred file chunks');
  }
  result.summary = await rpc({Op: 'summary'});
  if (result.summary.file_heads !== result.rows.length + 1 || result.summary.file_heads !== beforeIdle.file_heads) throw Error('Unexpected document duplication');
  if (options['latency-probe']) {
    for (const delay of [0, 100, 300]) for (let i = 0; i < 5; i++) {
      const sample = await rpc({Op: 'sync', DelayMS: delay}); result.latency_probe.push(sample);
      if (sample.metrics.chunks_up || sample.metrics.chunks_down) throw Error('Latency probe transferred chunks');
    }
  }
  result.final_relay = await api('GET', '/v1/sync/settings');
  result.finished = new Date().toISOString(); persist();
} catch (e) {
  result.error = e.message; process.exitCode = 1;
  console.error('Benchmark failed: ' + e.message);
} finally {
  active = false;
  if (context) await context.close().catch(() => {});
  if (worker) { worker.stdin.end(); worker.kill('SIGTERM'); }
  if (admin) {
    // Remove holding peers first, while both delivered stores can release the queue.
    for (const device of devices) {
      try { await api('DELETE', '/v1/sync/relay/devices/' + device, undefined, true); result.cleanup.removed_devices.push(device); }
      catch (e) { result.cleanup.errors.push(e.message); }
    }
    try { result.cleanup.settings = await api('GET', '/v1/sync/settings', undefined, true); }
    catch (e) { result.cleanup.errors.push(e.message); }
    for (const device of [...sessions].filter(x => x !== admin.device).concat(admin.device)) {
      try { await api('DELETE', '/v1/devices/' + device, undefined, true); result.cleanup.revoked_sessions.push(device); }
      catch (e) { result.cleanup.errors.push(e.message); }
    }
  }
  if (server && server.exitCode === null) {
    const stopped = once(server, 'exit'); server.kill('SIGTERM');
    await Promise.race([stopped, sleep(6000)]);
    if (server.exitCode === null) server.kill('SIGKILL');
  }
  password = undefined; persist();
}

function csv(value) { return '"' + String(value).replaceAll('"', '""') + '"'; }
const columns = ['direction', 'name', 'type', 'bytes', 'upload_seconds', 'receive_seconds', 'upload_requests', 'receive_requests', 'browser_foreground_seconds', 'integrity_ok', 'go_production_sync_seconds', 'go_settings_read_seconds'];
const rows = result.rows.map(row => {
  const b = row.browser_sync, go = row.source || row.destination;
  const up = row.direction === 'browser-to-go' ? b : {...go, ...go.metrics}, down = row.direction === 'browser-to-go' ? {...go, ...go.metrics} : b;
  return [row.direction, row.name, row.type, row.size, up.seconds, down.seconds, up.requests, down.requests, b.foreground_span_seconds, row.integrity_ok, go.production_sync_ms/1000, go.settings_read_ms/1000];
});
writeFileSync(join(dir, 'transfers.csv'), [columns, ...rows].map(row => row.map(csv).join(',')).join('\n') + '\n');
const median = numbers => { const sorted = [...numbers].sort((a, b) => a - b), n = sorted.length; return n % 2 ? sorted[(n - 1) / 2] : (sorted[n / 2 - 1] + sorted[n / 2]) / 2; };
const report = ['# Relay benchmark', '', `Relay: ${base || 'not started'}. Mode: ${result.mode}. Started: ${result.started}.`, '',
  `${result.rows.filter(x => x.integrity_ok).length}/${result.rows.length} received files passed SHA-256 verification. ${history} synthetic initial folder revisions; four offline holding peers.`, '',
  '| Direction | File | Bytes | Upload sync | Receive sync |', '| --- | --- | ---: | ---: | ---: |',
  ...rows.map(x => `| ${x[0]} | ${x[1]} | ${x[3]} | ${x[4].toFixed(3)}s | ${x[5].toFixed(3)}s |`), '',
  'Browser times include the original 200 ms wait, 100 ms polling, 150 ms settling, and UI automation. Go harness times include one final settings check; production_sync_ms and settings_read_ms in results.json and CSV separate these durations. HTTP error traces show rate limiting separately. Both exclude import and polling delays. This run uses a fresh synthetic account, rather than the private starting collection in the October 2026 reports.', '',
  `Local request burst allowance: ${result.local_rate_burst ?? 'hosted configuration unchanged'}. Synthetic history and optional injected latency are recorded in results.json. Request traces contain methods, paths, statuses and elapsed times; no credentials or account bytes.`, '',
  `Cleanup: ${result.cleanup.removed_devices.length} benchmark devices removed; ${result.cleanup.revoked_sessions.length} sessions revoked. ${result.cleanup.errors.length} cleanup errors. The server retains the synthetic account unlock record and revision identities; it has no account-delete endpoint.`, '',];
const idle = result.checks.filter(x => x.check.startsWith('no-edit'));
if (idle.length) report.push(`No-edit sync medians: Go ${median(idle.map(x => x.go.seconds)).toFixed(3)}s; browser UI ${median(idle.map(x => x.browser.seconds)).toFixed(3)}s.`, '');
if (result.latency_probe.length) {
  report.push('| Delay per request | Median Go elapsed | Production sync | Requests |', '| --- | ---: | ---: | ---: |');
  for (const delay of [0, 100, 300]) {
    const samples = result.latency_probe.filter(x => x.injected_delay_ms === delay);
    report.push(`| ${delay} ms | ${median(samples.map(x => x.seconds)).toFixed(3)}s | ${median(samples.map(x => x.production_sync_ms))/1000}s | ${samples[0].metrics.requests} |`);
  }
  report.push('');
}
if (result.error) report.push('Run failed: ' + result.error, '');
writeFileSync(join(dir, 'README.md'), report.join('\n'));
if (result.cleanup.errors.length) { console.error('Some benchmark cleanup requests failed; see results.json.'); process.exitCode = 1; }
console.log(`Saved results.json, transfers.csv and README.md to ${dir}`);
