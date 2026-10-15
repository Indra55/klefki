(() => {
  'use strict';
  const chapters = JSON.parse(document.getElementById('chapters-data').textContent);
  const byId = new Map(chapters.map(c => [c.id, c]));
  const $ = id => document.getElementById(id);
  const get = (key, fallback) => { try { return localStorage.getItem('klefki-guide-' + key) ?? fallback; } catch { return fallback; } };
  const set = (key, value) => { try { localStorage.setItem('klefki-guide-' + key, value); } catch { /* file origins may deny persistent preferences */ } };
  let read;
  try { read = new Set(JSON.parse(get('read', '[]')).filter(id => byId.has(id))); } catch { read = new Set(); }
  let current = chapters[0].id;
  let font = Number(get('font', '0'));
  if (![0, 1, 2].includes(font)) font = 0;
  const texts = new Map(chapters.map(c => [c.id, $(c.id).textContent.replace(/\s+/g, ' ').trim()]));
  const isDark = get('theme', window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light') === 'dark';
  document.body.classList.toggle('dark', isDark);
  const syncTheme = () => { $('theme').textContent = document.body.classList.contains('dark') ? 'Light' : 'Dark'; };
  syncTheme();
  const syncFont = () => document.documentElement.style.setProperty('--size', [17, 19, 21][font] + 'px');
  syncFont();

  function renderNavigation() {
    const query = $('search').value.trim().toLowerCase();
    const matches = chapters.filter(c => !query || texts.get(c.id).toLowerCase().includes(query));
    $('chapter-list').replaceChildren();
    for (const c of matches) {
      const a = document.createElement('a');
      a.href = '#' + c.id;
      a.className = 'chapter-link';
      if (c.id === current) a.setAttribute('aria-current', 'page');
      const number = document.createElement('span');
      number.className = 'number';
      number.textContent = c.id.slice(0, 2);
      a.append(number, document.createTextNode(c.title));
      if (read.has(c.id)) {
        const check = document.createElement('span');
        check.className = 'check'; check.textContent = '✓'; check.setAttribute('aria-label', 'Read');
        a.append(check);
      }
      if (query) {
        const text = texts.get(c.id), position = text.toLowerCase().indexOf(query);
        const snippet = document.createElement('span');
        snippet.className = 'search-snippet';
        snippet.textContent = (position > 40 ? '…' : '') + text.slice(Math.max(0, position - 40), position + query.length + 70) + '…';
        a.append(snippet);
      }
      $('chapter-list').append(a);
    }
    $('search-status').textContent = query ? matches.length + ' matching chapters' : chapters.length + ' chapters · ' + read.size + ' read';
  }

  function navigate() {
    let fragment;
    try { fragment = decodeURIComponent(location.hash.slice(1)); } catch { fragment = ''; }
    const anchor = $(fragment);
    const selected = byId.has(fragment) ? fragment : anchor?.closest('.chapter')?.id;
    current = selected || (byId.has(get('last', '')) ? get('last', '') : chapters[0].id);
    const chapter = byId.get(current), index = chapters.indexOf(chapter);
    document.querySelectorAll('.chapter').forEach(a => { a.hidden = a.id !== current; });
    $('chapter-meta').textContent = 'CHAPTER ' + chapter.id.slice(0, 2) + ' / 17 · ' + chapter.minutes + ' MIN READ';
    document.title = chapter.title + ' · Klefki technical guide';
    $('previous').disabled = index === 0;
    $('next').disabled = index === chapters.length - 1;
    $('markdown-link').href = chapter.file;
    $('mark-read').setAttribute('aria-pressed', String(read.has(current)));
    $('mark-read').textContent = read.has(current) ? 'Read ✓' : 'Mark as read';
    $('section-list').replaceChildren();
    $(current).querySelectorAll('h2, h3').forEach(h => {
      const link = document.createElement('a'); link.href = '#' + h.id; link.textContent = h.textContent;
      $('section-list').append(link);
    });
    set('last', current);
    renderNavigation();
    $('menu').setAttribute('aria-expanded', 'false');
    document.querySelector('.sidebar').classList.remove('open');
    if (selected && fragment !== current) requestAnimationFrame(() => anchor.scrollIntoView({behavior: 'instant'}));
    else window.scrollTo({top: 0, behavior: 'instant'});
  }

  $('search').addEventListener('input', renderNavigation);
  $('theme').addEventListener('click', () => {
    document.body.classList.toggle('dark');
    set('theme', document.body.classList.contains('dark') ? 'dark' : 'light'); syncTheme();
  });
  $('font').addEventListener('click', () => { font = (font + 1) % 3; set('font', String(font)); syncFont(); });
  $('print').addEventListener('click', () => window.print());
  const step = direction => {
    const index = chapters.findIndex(c => c.id === current) + direction;
    if (chapters[index]) location.hash = chapters[index].id;
  };
  $('previous').addEventListener('click', () => step(-1));
  $('next').addEventListener('click', () => step(1));
  $('mark-read').addEventListener('click', () => {
    if (read.has(current)) read.delete(current); else read.add(current);
    set('read', JSON.stringify([...read]));
    $('mark-read').setAttribute('aria-pressed', String(read.has(current)));
    $('mark-read').textContent = read.has(current) ? 'Read ✓' : 'Mark as read'; renderNavigation();
  });
  $('menu').addEventListener('click', () => {
    const open = document.querySelector('.sidebar').classList.toggle('open');
    $('menu').setAttribute('aria-expanded', String(open));
    if (open) $('search').focus();
  });
  $('reading').addEventListener('click', event => {
    if (event.target.tagName === 'IMG') {
      $('large-diagram').src = event.target.src;
      $('large-diagram').alt = event.target.alt;
      $('diagram-dialog').showModal();
    }
  });
  $('close-diagram').addEventListener('click', () => $('diagram-dialog').close());
  document.querySelectorAll('.chapter img').forEach(img => {
    img.tabIndex = 0; img.setAttribute('role', 'button');
    img.setAttribute('aria-label', img.alt + '. Open enlarged diagram.');
    img.addEventListener('keydown', event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); img.click(); } });
  });
  document.querySelectorAll('.chapter a[href^="https://"]').forEach(a => { a.target = '_blank'; a.rel = 'noopener noreferrer'; });
  window.addEventListener('hashchange', navigate);
  window.addEventListener('keydown', event => {
    if (event.key === 'Escape') { document.querySelector('.sidebar').classList.remove('open'); $('menu').setAttribute('aria-expanded', 'false'); }
    if (event.target.closest('input, textarea, button, a') || event.altKey || event.ctrlKey || event.metaKey || $('diagram-dialog').open) return;
    if (event.key === 'ArrowRight') step(1);
    if (event.key === 'ArrowLeft') step(-1);
  });
  navigate();
})();
