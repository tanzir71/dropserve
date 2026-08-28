const search = document.querySelector('#app-search');
const grid = document.querySelector('#app-grid');
const empty = document.querySelector('#empty-state');
const errorState = document.querySelector('#error-state');
const count = document.querySelector('#app-count');
const sharingToggle = document.querySelector('#sharing-toggle');
const sharingPanel = document.querySelector('#sharing-panel');
const sharingClose = document.querySelector('#sharing-close');
const addonsToggle = document.querySelector('#addons-toggle');
const addonsPanel = document.querySelector('#addons-panel');
const addonsClose = document.querySelector('#addons-close');
const addonsList = document.querySelector('#addons-list');
const sharingURLs = document.querySelector('#sharing-urls');
const localHTTPSState = document.querySelector('#local-https-state');
const localHTTPSToggle = document.querySelector('#local-https-toggle');
const localTrustToggle = document.querySelector('#local-trust-toggle');
const localRootDownload = document.querySelector('#local-root-download');
const qrDialog = document.querySelector('#qr-dialog');
const qrImage = document.querySelector('#qr-image');
const qrAddress = document.querySelector('#qr-address');
const qrCopy = document.querySelector('#qr-copy');
const funnelDialog = document.querySelector('#funnel-dialog');
const funnelSlug = document.querySelector('#funnel-slug');
const funnelConfirmation = document.querySelector('#funnel-confirmation');
const funnelConfirm = document.querySelector('#funnel-confirm');
const funnelState = document.querySelector('#funnel-state');
const warningNotice = document.querySelector('#port-warning');
const publicSharingWarning = document.querySelector('#public-sharing-warning');
const addressChangeWarning = document.querySelector('#address-change-warning');
const logDialog = document.querySelector('#log-dialog');
const logTitle = document.querySelector('#log-title');
const logState = document.querySelector('#log-state');
const logOutput = document.querySelector('#log-output');
const logRefresh = document.querySelector('#log-refresh');
const databaseDialog = document.querySelector('#database-dialog');
const databaseTitle = document.querySelector('#database-title');
const databaseState = document.querySelector('#database-state');
const databaseContent = document.querySelector('#database-content');

let apps = [];
let visible = [];
let selected = 0;
let currentQRURL = '';
let currentLogApp = null;
let currentFunnelApp = null;
let csrfToken = '';
let activeFunnels = new Map();

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

function addonCard(addon) {
  const card = document.createElement('section');
  card.className = 'addon-card';
  const heading = document.createElement('h3');
  heading.textContent = `${addon.title} ${addon.version || ''}`.trim();
  const description = document.createElement('p');
  description.textContent = addon.description || 'Optional Dropserve runtime.';
  const state = document.createElement('p');
  state.className = 'addon-state';
  if (addon.busy) state.textContent = `Working… ${addon.progress || 0}%`;
  else if (addon.running) state.textContent = 'Installed · running';
  else if (addon.installed) state.textContent = 'Installed · stopped';
  else state.textContent = addon.available ? 'Not installed' : (addon.message || 'Not available on this platform');
  const actions = document.createElement('div');
  actions.className = 'addon-actions';
  if (addon.available && !addon.installed) actions.append(actionButton('Install', 'install-addon'));
  if (addon.installed && addon.name !== 'php') actions.append(actionButton(addon.running ? 'Stop' : 'Start', addon.running ? 'stop-addon' : 'start-addon'));
  if (addon.installed && addon.name === 'php' && !addon.running) actions.append(actionButton('Start', 'start-addon'));
  if (addon.installed) actions.append(actionButton('Remove', 'remove-addon'));
  for (const button of actions.querySelectorAll('button')) {
    button.addEventListener('click', async () => {
      const action = button.dataset.action.replace('-addon', '');
      if (action === 'remove') {
        const warning = addon.name === 'php'
          ? 'Removing the PHP pack deletes the downloaded PHP files. Your apps and their files are untouched.'
          : `Removing ${addon.title} deletes the downloaded ${addon.title} files and Dropserve-managed database data. Your apps and their files are untouched.`;
        const confirmed = window.confirm(warning);
        if (!confirmed) return;
      }
      button.disabled = true;
      state.textContent = action === 'install' ? 'Downloading and verifying…' : `${action[0].toUpperCase()}${action.slice(1)}ing…`;
      try {
        await postSharing(`/_dropserve/api/addons/${encodeURIComponent(addon.name)}`, { action });
        await refreshAddons();
        await loadApps();
        await loadStatus();
      } catch (error) {
        state.textContent = error.message;
        button.disabled = false;
      }
    });
  }
  if (addon.connection) {
    const connection = document.createElement('code');
    connection.className = 'addon-connection';
    connection.textContent = addon.connection;
    connection.title = 'Click to copy connection string';
    connection.addEventListener('click', () => copyText(addon.connection, connection));
    card.append(heading, description, state, connection, actions);
  } else {
    card.append(heading, description, state, actions);
  }
  return card;
}

async function refreshAddons() {
  const response = await fetch('/_dropserve/api/addons');
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  const addons = await response.json();
  addonsList.replaceChildren(...addons.map(addonCard));
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

function databaseCell(value) {
  if (value === null) return 'NULL';
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}

async function showDatabase(item, file) {
  databaseTitle.textContent = `${file} · ${item.name || item.slug}`;
  databaseState.textContent = 'Loading the first 100 rows from each table…';
  databaseContent.replaceChildren();
  if (typeof databaseDialog.showModal === 'function') databaseDialog.showModal();
  else databaseDialog.setAttribute('open', '');
  try {
    const response = await fetch(`/_dropserve/api/databases/${encodeURIComponent(item.slug)}?file=${encodeURIComponent(file)}`);
    if (!response.ok) throw new Error((await response.text()).trim() || `HTTP ${response.status}`);
    const snapshot = await response.json();
    const tables = snapshot.tables || [];
    databaseState.textContent = tables.length ? `${tables.length} ${tables.length === 1 ? 'table' : 'tables'} · read-only` : 'This database has no user tables.';
    const sections = tables.map(table => {
      const section = document.createElement('section');
      const heading = document.createElement('h3');
      heading.textContent = `${table.name} · ${(table.rows || []).length} rows shown`;
      const scroller = document.createElement('div');
      scroller.className = 'database-table-scroll';
      const grid = document.createElement('table');
      const header = document.createElement('thead');
      const headerRow = document.createElement('tr');
      (table.columns || []).forEach(column => {
        const cell = document.createElement('th');
        cell.scope = 'col';
        cell.textContent = column;
        headerRow.append(cell);
      });
      header.append(headerRow);
      const body = document.createElement('tbody');
      (table.rows || []).forEach(row => {
        const tableRow = document.createElement('tr');
        row.forEach(value => {
          const cell = document.createElement('td');
          cell.textContent = databaseCell(value);
          tableRow.append(cell);
        });
        body.append(tableRow);
      });
      grid.append(header, body);
      scroller.append(grid);
      section.append(heading, scroller);
      return section;
    });
    databaseContent.replaceChildren(...sections);
  } catch (error) {
    databaseState.textContent = error.message || 'Dropserve could not read this database.';
  }
}

function appCard(item, index) {
  const article = document.createElement('article');
  article.className = 'app-card';
  article.dataset.selected = String(index === selected);
  article.dataset.status = item.status || 'ready';
  const pathURL = item.urls?.path || `/${encodeURIComponent(item.slug)}/`;
  const ownURL = item.urls?.own || '';
  const targetURL = item.prefers_own_port && ownURL ? ownURL : pathURL;
  const publicShare = activeFunnels.get(item.slug);
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
  if (item.prefers_own_port && ownURL) {
    const rescue = document.createElement('p');
    rescue.className = 'own-port-note';
    rescue.textContent = 'This app expects to live at the root, so Dropserve is serving it on its own port.';
    link.append(rescue);
  }
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
  const openLabel = item.prefers_own_port && ownURL ? 'Open on its own port' : 'Open';
  menu.append(actionButton(openLabel, 'open'));
  if (ownURL && !item.prefers_own_port) menu.append(actionButton('Open on its own port', 'open-own'));
  if (item.prefers_own_port && ownURL) menu.append(actionButton('Use the short URL anyway', 'open-path'));
  menu.append(actionButton('Copy link', 'copy'), actionButton('Show QR', 'qr'));
  if (publicShare) {
    if (publicShare.url) {
      menu.append(
        actionButton('Open public link', 'open-public'),
        actionButton('Copy public link', 'copy-public'),
        actionButton('Show public QR', 'qr-public'),
      );
    }
    menu.append(actionButton('Stop public sharing', 'funnel-stop'));
  } else {
    menu.append(actionButton('Share publicly…', 'funnel-start'));
  }
  if (item.type === 'command') menu.append(actionButton('View logs', 'logs'));
  (item.databases || []).forEach(file => {
    const browse = actionButton(`Browse database · ${file}`, 'database');
    browse.dataset.file = file;
    menu.append(browse);
  });
  toggle.addEventListener('click', event => {
    event.stopPropagation();
    const opening = menu.hidden;
    document.querySelectorAll('.card-actions').forEach(other => { other.hidden = true; });
    document.querySelectorAll('.card-actions-toggle').forEach(other => { other.setAttribute('aria-expanded', 'false'); });
    menu.hidden = !opening;
    toggle.setAttribute('aria-expanded', String(opening));
  });
  menu.addEventListener('click', async event => {
    const button = event.target.closest('[data-action]');
    if (!button) return;
    const absoluteURL = new URL(targetURL, window.location.href).href;
    if (button.dataset.action === 'open') window.location.assign(absoluteURL);
    if (button.dataset.action === 'open-own' && ownURL) window.location.assign(new URL(ownURL, window.location.href).href);
    if (button.dataset.action === 'open-path') window.location.assign(new URL(pathURL, window.location.href).href);
    if (button.dataset.action === 'copy') copyText(absoluteURL, button);
    if (button.dataset.action === 'qr') showQR(absoluteURL, item.name || item.slug);
    if (button.dataset.action === 'open-public' && publicShare?.url) window.location.assign(publicShare.url);
    if (button.dataset.action === 'copy-public' && publicShare?.url) await copyText(publicShare.url, button);
    if (button.dataset.action === 'qr-public' && publicShare?.url) showQR(publicShare.url, `${item.name || item.slug} public link`);
    if (button.dataset.action === 'funnel-start') showFunnelConfirmation(item);
    if (button.dataset.action === 'funnel-stop') await changeFunnel(item, false);
    if (button.dataset.action === 'logs') showLogs(item);
    if (button.dataset.action === 'database') showDatabase(item, button.dataset.file);
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
  const labels = {
    loopback: 'This computer',
    lan: 'Local network',
    mdns: 'Easy local address',
    'https-loopback': 'This computer · HTTPS',
    'https-lan': 'Local network · HTTPS',
    'https-mdns': 'Easy local address · HTTPS',
    tailscale: 'Tailscale',
    current: 'Current address',
  };
  const details = document.createElement('div');
  details.className = 'sharing-details';
  const name = document.createElement('span');
  name.className = 'sharing-name';
  name.textContent = labels[item.kind] || item.kind;
  details.append(name);
  if (!item.url) {
    const message = document.createElement('p');
    message.textContent = item.message || 'This sharing option is not available yet.';
    details.append(message);
    if (item.kind === 'tailscale' && item.message?.toLowerCase().includes('not installed')) {
      const install = document.createElement('a');
      install.href = 'https://tailscale.com/download';
      install.target = '_blank';
      install.rel = 'noopener';
      install.textContent = 'Get Tailscale';
      details.append(install);
    }
    row.append(details);
    return row;
  }
  const link = document.createElement('a');
  link.href = item.url;
  link.textContent = item.url;
  details.append(link);
  const copy = actionButton('Copy', 'copy');
  const qr = actionButton('QR', 'qr');
  copy.className = 'mini-button';
  qr.className = 'mini-button';
  copy.addEventListener('click', () => copyText(item.url, copy));
  qr.addEventListener('click', () => showQR(item.url, 'Open Dropserve'));
  row.append(details, copy, qr);
  if (item.kind === 'tailscale') {
    const secure = item.url.startsWith('https://');
    const toggle = actionButton(secure ? 'Turn off tailnet HTTPS' : 'Use HTTPS anywhere', 'tailscale-serve');
    toggle.className = 'mini-button wide-action';
    toggle.addEventListener('click', async () => {
      toggle.disabled = true;
      try {
        await postSharing('/_dropserve/api/sharing/tailscale', { enabled: !secure });
        await reloadSharingState();
      } catch (error) {
        toggle.textContent = error.message;
        toggle.disabled = false;
      }
    });
    row.append(toggle);
  }
  return row;
}

async function postSharing(path, payload) {
  if (!csrfToken) await loadStatus();
  if (!csrfToken) throw new Error('Refresh the dashboard and try again.');
  const response = await fetch(path, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Dropserve-CSRF': csrfToken,
    },
    body: JSON.stringify(payload),
  });
  if (!response.ok) {
    const detail = (await response.text()).trim();
    throw new Error(detail || `HTTP ${response.status}`);
  }
}

function showFunnelConfirmation(item) {
  currentFunnelApp = item;
  funnelSlug.textContent = item.slug;
  funnelConfirmation.value = '';
  funnelConfirm.disabled = true;
  funnelState.textContent = '';
  document.querySelector('#funnel-title').textContent = `Share ${item.name || item.slug} publicly?`;
  if (typeof funnelDialog.showModal === 'function') funnelDialog.showModal();
  else funnelDialog.setAttribute('open', '');
  funnelConfirmation.focus();
}

async function changeFunnel(item, enabled, confirmation = '') {
  try {
    await postSharing(`/_dropserve/api/sharing/funnel/${encodeURIComponent(item.slug)}`, { enabled, confirmation });
    await reloadSharingState();
    if (enabled) funnelDialog.close();
  } catch (error) {
    if (enabled) funnelState.textContent = error.message;
    else {
      warningNotice.querySelector('p').textContent = error.message;
      warningNotice.hidden = false;
    }
  }
}

async function refreshSharing() {
  const response = await fetch('/_dropserve/api/urls');
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  const items = await response.json();
  sharingURLs.replaceChildren(...items.map(sharingRow));
}

async function reloadSharingState() {
  await Promise.all([loadStatus(), refreshSharing()]);
}

function renderLocalHTTPS(status = {}) {
  localHTTPSToggle.textContent = status.enabled ? 'Turn off local HTTPS' : 'Enable local HTTPS';
  localHTTPSToggle.dataset.enabled = String(Boolean(status.enabled));
  localTrustToggle.hidden = !status.root_available;
  localRootDownload.hidden = !status.root_available;
  localTrustToggle.textContent = status.trust_installed ? 'Remove local trust' : 'Trust on this computer';
  localTrustToggle.dataset.installed = String(Boolean(status.trust_installed));
  if (status.warning) localHTTPSState.textContent = status.warning;
  else if (status.enabled) localHTTPSState.textContent = `Local HTTPS is on at port ${status.port}. HTTP stays available.`;
  else localHTTPSState.textContent = 'Local HTTPS is off. HTTP stays the default.';
}

function setSharingOpen(open) {
  sharingPanel.hidden = !open;
  sharingToggle.setAttribute('aria-expanded', String(open));
  if (open) {
    setAddonsOpen(false);
    sharingClose.focus();
  }
}

function setAddonsOpen(open) {
  addonsPanel.hidden = !open;
  addonsToggle.setAttribute('aria-expanded', String(open));
  if (open) {
    setSharingOpen(false);
    refreshAddons().catch(() => { addonsList.textContent = 'Dropserve could not load add-ons.'; });
    addonsClose.focus();
  }
}

sharingToggle.addEventListener('click', () => setSharingOpen(sharingPanel.hidden));
sharingClose.addEventListener('click', () => setSharingOpen(false));
addonsToggle.addEventListener('click', () => setAddonsOpen(addonsPanel.hidden));
addonsClose.addEventListener('click', () => setAddonsOpen(false));
qrCopy.addEventListener('click', () => copyText(currentQRURL, qrCopy));
logRefresh.addEventListener('click', refreshLogs);
localHTTPSToggle.addEventListener('click', async () => {
  localHTTPSToggle.disabled = true;
  try {
    await postSharing('/_dropserve/api/https', { enabled: localHTTPSToggle.dataset.enabled !== 'true' });
    await reloadSharingState();
  } catch (error) {
    localHTTPSState.textContent = error.message;
  } finally {
    localHTTPSToggle.disabled = false;
  }
});
localTrustToggle.addEventListener('click', async () => {
  localTrustToggle.disabled = true;
  try {
    await postSharing('/_dropserve/api/trust', { installed: localTrustToggle.dataset.installed !== 'true' });
    await loadStatus();
  } catch (error) {
    localHTTPSState.textContent = error.message;
  } finally {
    localTrustToggle.disabled = false;
  }
});
funnelConfirmation.addEventListener('input', () => {
  funnelConfirm.disabled = !currentFunnelApp || funnelConfirmation.value !== currentFunnelApp.slug;
  funnelState.textContent = '';
});
funnelConfirm.addEventListener('click', async () => {
  if (!currentFunnelApp) return;
  funnelConfirm.disabled = true;
  funnelState.textContent = 'Creating the temporary public link…';
  await changeFunnel(currentFunnelApp, true, funnelConfirmation.value);
  if (funnelDialog.open) funnelConfirm.disabled = funnelConfirmation.value !== currentFunnelApp.slug;
});
funnelDialog.addEventListener('close', () => { currentFunnelApp = null; });
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
  if (event.key === 'Escape') {
    setSharingOpen(false);
    setAddonsOpen(false);
  }
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

async function loadStatus() {
  const response = await fetch('/_dropserve/api/status');
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  const status = await response.json();
  csrfToken = status.csrf_token || csrfToken;
  activeFunnels = new Map((status.sharing?.public || []).map(entry => [entry.slug, entry]));
  renderLocalHTTPS(status.https);
  if (apps.length) render();

  if (status.network?.change) {
    addressChangeWarning.querySelector('[data-old-lan-ip]').textContent = status.network.change.old_lan_ip;
    addressChangeWarning.querySelector('[data-new-lan-ip]').textContent = status.network.change.new_lan_ip;
    addressChangeWarning.hidden = false;
    const dismiss = addressChangeWarning.querySelector('button');
    if (!dismiss.dataset.bound) {
      dismiss.dataset.bound = 'true';
      dismiss.addEventListener('click', async () => {
        const dismissal = await fetch('/_dropserve/api/network-change/dismiss', {
          method: 'POST',
          headers: { 'X-Dropserve-CSRF': csrfToken },
        });
        if (dismissal.ok) addressChangeWarning.hidden = true;
      });
    }
  }

  const publicWarnings = (status.warnings || []).filter(warning => warning.startsWith('public_sharing_active:'));
  const otherWarnings = (status.warnings || []).filter(warning => !warning.startsWith('public_sharing_active:'));
  publicSharingWarning.hidden = publicWarnings.length === 0;
  if (publicWarnings.length) {
    publicSharingWarning.querySelector('p').textContent = publicWarnings.map(warning => warning.replace('public_sharing_active:', 'Public sharing is active.')).join(' ');
  }
  warningNotice.hidden = otherWarnings.length === 0;
  if (otherWarnings.length) {
    warningNotice.querySelector('p').textContent = otherWarnings.join(' ');
    const diagnose = warningNotice.querySelector('button');
    if (!diagnose.dataset.bound) {
      diagnose.dataset.bound = 'true';
      diagnose.addEventListener('click', () => window.location.assign('/_dropserve/api/status'));
    }
  }
  return status;
}

loadStatus().catch(() => {});
refreshSharing().catch(() => { sharingURLs.textContent = 'No verified address is available yet.'; });
