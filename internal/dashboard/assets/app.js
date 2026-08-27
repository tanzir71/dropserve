const search = document.querySelector('#app-search');
const grid = document.querySelector('#app-grid');
const empty = document.querySelector('#empty-state');
const errorState = document.querySelector('#error-state');
const count = document.querySelector('#app-count');
const sharingToggle = document.querySelector('#sharing-toggle');
const sharingPanel = document.querySelector('#sharing-panel');
const sharingClose = document.querySelector('#sharing-close');
const sharingURLs = document.querySelector('#sharing-urls');
const qrDialog = document.querySelector('#qr-dialog');
const qrImage = document.querySelector('#qr-image');
const qrAddress = document.querySelector('#qr-address');
const qrCopy = document.querySelector('#qr-copy');
const warningNotice = document.querySelector('#port-warning');
const logDialog = document.querySelector('#log-dialog');
const logTitle = document.querySelector('#log-title');
const logState = document.querySelector('#log-state');
const logOutput = document.querySelector('#log-output');
const logRefresh = document.querySelector('#log-refresh');

let apps = [];
let visible = [];
let selected = 0;
let currentQRURL = '';
let currentLogApp = null;

const palette = ['#156b50', '#3d5d9a', '#9a5b3d', '#77519d', '#a06d18', '#327b82'];

function initials(name) {
  return name.split(/\s+/).filter(Boolean).slice(0, 2).map(part => part[0]).join('').toUpperCase();
}

function colour(slug) {
  let hash = 0;
  for (const letter of slug) hash = ((hash << 5) - hash + letter.charCodeAt(0)) | 0;
  return palette[Math.abs(hash) % palette.length];
}

async function copyText(value, button) {
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    const helper = document.createElement('textarea');
    helper.value = value;
    helper.setAttribute('readonly', '');
    helper.style.position = 'fixed';
    helper.style.opacity = '0';
    document.body.append(helper);
    helper.select();
    document.execCommand('copy');
    helper.remove();
  }
  if (button) {
    const original = button.textContent;
    button.textContent = 'Copied';
    window.setTimeout(() => { button.textContent = original; }, 1200);
  }
}

function showQR(targetURL, label = 'Open this address') {
  currentQRURL = new URL(targetURL, window.location.href).href;
  qrImage.src = `/_dropserve/api/qr?url=${encodeURIComponent(currentQRURL)}`;
  qrAddress.textContent = currentQRURL;
  document.querySelector('#qr-title').textContent = label;
  if (typeof qrDialog.showModal === 'function') qrDialog.showModal();
  else qrDialog.setAttribute('open', '');
}

function actionButton(label, action) {
  const button = document.createElement('button');
  button.type = 'button';
  button.textContent = label;
  button.dataset.action = action;
  return button;
}

function lastLogLines(logs, maximum = 50) {
  return String(logs || '').trimEnd().split(/\r?\n/).slice(-maximum).join('\n');
}

function fetchLogs(item) {
  return fetch(`/_dropserve/api/logs/${encodeURIComponent(item.slug)}`)
    .then(response => {
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      return response.json();
    });
}

function refreshLogs() {
  if (!currentLogApp) return;
  logState.textContent = 'Loading the latest output…';
  fetchLogs(currentLogApp)
    .then(snapshot => {
      const attempts = snapshot.attempts === 1 ? '1 start' : `${snapshot.attempts || 0} starts`;
      logState.textContent = `${snapshot.status || currentLogApp.status || 'ready'} · ${attempts}`;
      logOutput.textContent = snapshot.logs || 'This app has not written any output yet.';
    })
    .catch(() => {
      logState.textContent = 'Dropserve could not refresh these logs.';
      logOutput.textContent = 'Try again in a moment.';
    });
}

function showLogs(item) {
  currentLogApp = item;
  logTitle.textContent = `${item.name || item.slug} logs`;
  logState.textContent = 'Loading the latest output…';
  logOutput.textContent = 'Loading…';
  if (typeof logDialog.showModal === 'function') logDialog.showModal();
  else logDialog.setAttribute('open', '');
  refreshLogs();
}

function appCard(item, index) {
  const article = document.createElement('article');
  article.className = 'app-card';
  article.dataset.selected = String(index === selected);
  article.dataset.status = item.status || 'ready';
  const targetURL = item.prefers_own_port && item.urls?.own
    ? item.urls.own
    : (item.urls?.path || `/${encodeURIComponent(item.slug)}/`);
  const link = document.createElement('a');
  link.href = targetURL;
  link.dataset.appLink = '';

  const icon = document.createElement('div');
  icon.className = 'app-icon';
  icon.style.setProperty('--icon', item.icon_color || colour(item.slug));
  if (item.icon_kind === 'image') {
    const image = document.createElement('img');
    image.src = item.icon;
    image.alt = '';
    icon.append(image);
  } else {
    icon.textContent = item.icon || initials(item.name || item.slug);
  }
  const name = document.createElement('h3');
  name.textContent = item.name || item.slug;
  const description = document.createElement('p');
  if (item.status === 'crashed') description.textContent = 'This app stopped after five starts. Its latest output is below.';
  else if (item.status === 'needs-runtime') description.textContent = 'This app needs its runtime installed before it can open.';
  else if (item.status === 'stopped') description.textContent = 'This app starts when you open it.';
  else description.textContent = item.description || item.heading || 'Ready to open on this device.';
  const meta = document.createElement('div');
  meta.className = 'card-meta';
  meta.innerHTML = '<span class="online-dot"></span>';
  meta.append(document.createTextNode(`${item.type || 'static'} · ${item.status || 'ready'}`));
  link.append(icon, name, description);
  if (item.status === 'crashed') {
    const preview = document.createElement('pre');
    preview.className = 'crash-preview';
    preview.textContent = 'Loading the last 50 lines…';
    link.append(preview);
    fetchLogs(item)
      .then(snapshot => { preview.textContent = lastLogLines(snapshot.logs) || 'No error output was captured.'; })
      .catch(() => { preview.textContent = 'Logs are temporarily unavailable.'; });
  }
  link.append(meta);

  const toggle = document.createElement('button');
  toggle.type = 'button';
  toggle.className = 'card-actions-toggle';
  toggle.textContent = '⋯';
  toggle.setAttribute('aria-label', `Actions for ${item.name || item.slug}`);
  toggle.setAttribute('aria-expanded', 'false');
  const menu = document.createElement('div');
  menu.className = 'card-actions';
  menu.hidden = true;
  menu.append(actionButton('Open', 'open'), actionButton('Copy link', 'copy'), actionButton('Show QR', 'qr'));
  if (item.type === 'command') menu.append(actionButton('View logs', 'logs'));
  toggle.addEventListener('click', event => {
    event.stopPropagation();
    const opening = menu.hidden;
    document.querySelectorAll('.card-actions').forEach(other => { other.hidden = true; });
    document.querySelectorAll('.card-actions-toggle').forEach(other => { other.setAttribute('aria-expanded', 'false'); });
    menu.hidden = !opening;
    toggle.setAttribute('aria-expanded', String(opening));
  });
  menu.addEventListener('click', event => {
    const button = event.target.closest('[data-action]');
    if (!button) return;
    const absoluteURL = new URL(targetURL, window.location.href).href;
    if (button.dataset.action === 'open') window.location.assign(absoluteURL);
    if (button.dataset.action === 'copy') copyText(absoluteURL, button);
    if (button.dataset.action === 'qr') showQR(absoluteURL, item.name || item.slug);
    if (button.dataset.action === 'logs') showLogs(item);
    menu.hidden = true;
    toggle.setAttribute('aria-expanded', 'false');
  });
  article.append(link, toggle, menu);
  return article;
}

function render() {
  const query = search.value.trim().toLowerCase();
  visible = apps.filter(item => !query || [item.name, item.description, item.title, item.heading, item.slug].some(value => value?.toLowerCase().includes(query)));
  selected = Math.max(0, Math.min(selected, visible.length - 1));
  grid.replaceChildren(...visible.map(appCard));
  grid.setAttribute('aria-busy', 'false');
  empty.hidden = apps.length !== 0;
  errorState.hidden = true;
  count.textContent = apps.length === 1 ? '1 app' : `${apps.length} apps`;
}

function sharingRow(item) {
  const row = document.createElement('div');
  row.className = 'sharing-row';
  const link = document.createElement('a');
  link.href = item.url;
  link.textContent = item.url;
  const copy = actionButton('Copy', 'copy');
  const qr = actionButton('QR', 'qr');
  copy.className = 'mini-button';
  qr.className = 'mini-button';
  copy.addEventListener('click', () => copyText(item.url, copy));
  qr.addEventListener('click', () => showQR(item.url, 'Open Dropserve'));
  row.append(link, copy, qr);
  return row;
}

function setSharingOpen(open) {
  sharingPanel.hidden = !open;
  sharingToggle.setAttribute('aria-expanded', String(open));
  if (open) sharingClose.focus();
}

sharingToggle.addEventListener('click', () => setSharingOpen(sharingPanel.hidden));
sharingClose.addEventListener('click', () => setSharingOpen(false));
qrCopy.addEventListener('click', () => copyText(currentQRURL, qrCopy));
logRefresh.addEventListener('click', refreshLogs);
document.addEventListener('click', event => {
  if (!event.target.closest('.app-card')) {
    document.querySelectorAll('.card-actions').forEach(menu => { menu.hidden = true; });
    document.querySelectorAll('.card-actions-toggle').forEach(button => { button.setAttribute('aria-expanded', 'false'); });
  }
});

search.addEventListener('input', () => { selected = 0; render(); });
search.addEventListener('keydown', event => {
  if (!visible.length) return;
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault();
    selected = (selected + (event.key === 'ArrowDown' ? 1 : -1) + visible.length) % visible.length;
    render();
    grid.children[selected]?.scrollIntoView({ block: 'nearest' });
  }
  if (event.key === 'Enter') {
    event.preventDefault();
    const link = grid.children[selected]?.querySelector('[data-app-link]');
    if (event.ctrlKey || event.metaKey) window.open(link?.href, '_blank', 'noopener');
    else link?.click();
  }
});

document.addEventListener('keydown', event => {
  if (event.key === '/' && document.activeElement !== search) {
    event.preventDefault();
    search.focus();
  }
  if (event.key === 'Escape') setSharingOpen(false);
});

function loadApps() {
  return fetch('/_dropserve/api/apps')
    .then(response => {
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      return response.json();
    })
    .then(items => { apps = items; render(); })
    .catch(() => {
      grid.setAttribute('aria-busy', 'false');
      count.textContent = 'Unavailable';
      empty.hidden = true;
      errorState.hidden = false;
    });
}

loadApps();
if ('EventSource' in window) {
  const appEvents = new EventSource('/_dropserve/api/events');
  appEvents.addEventListener('apps-changed', loadApps);
}

fetch('/_dropserve/api/status')
  .then(response => {
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    return response.json();
  })
  .then(status => {
    if (!status.warnings?.length) return;
    warningNotice.querySelector('p').textContent = status.warnings.join(' ');
    warningNotice.hidden = false;
    warningNotice.querySelector('button').addEventListener('click', () => {
      window.location.assign('/_dropserve/api/status');
    });
  })
  .catch(() => {});

fetch('/_dropserve/api/urls')
  .then(response => {
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    return response.json();
  })
  .then(items => sharingURLs.replaceChildren(...items.map(sharingRow)))
  .catch(() => { sharingURLs.textContent = 'No verified address is available yet.'; });
