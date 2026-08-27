const search = document.querySelector('#app-search');
const grid = document.querySelector('#app-grid');
const empty = document.querySelector('#empty-state');
const errorState = document.querySelector('#error-state');
const count = document.querySelector('#app-count');

let apps = [];
let visible = [];
let selected = 0;

const palette = ['#156b50', '#3d5d9a', '#9a5b3d', '#77519d', '#a06d18', '#327b82'];

function initials(name) {
  return name.split(/\s+/).filter(Boolean).slice(0, 2).map(part => part[0]).join('').toUpperCase();
}

function colour(slug) {
  let hash = 0;
  for (const letter of slug) hash = ((hash << 5) - hash + letter.charCodeAt(0)) | 0;
  return palette[Math.abs(hash) % palette.length];
}

function appCard(item, index) {
  const article = document.createElement('article');
  article.className = 'app-card';
  article.dataset.selected = String(index === selected);
  const link = document.createElement('a');
  link.href = item.urls?.path || `/${encodeURIComponent(item.slug)}/`;
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
  description.textContent = item.description || 'Ready to open on this device.';
  const meta = document.createElement('div');
  meta.className = 'card-meta';
  meta.innerHTML = '<span class="online-dot"></span>';
  meta.append(document.createTextNode(item.type || 'static'));
  link.append(icon, name, description, meta);
  article.append(link);
  return article;
}

function render() {
  const query = search.value.trim().toLowerCase();
  visible = apps.filter(item => !query || [item.name, item.description, item.slug].some(value => value?.toLowerCase().includes(query)));
  selected = Math.max(0, Math.min(selected, visible.length - 1));
  grid.replaceChildren(...visible.map(appCard));
  grid.setAttribute('aria-busy', 'false');
  empty.hidden = apps.length !== 0;
  errorState.hidden = true;
  count.textContent = apps.length === 1 ? '1 app' : `${apps.length} apps`;
}

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
    const link = grid.children[selected]?.querySelector('a');
    if (event.ctrlKey || event.metaKey) window.open(link?.href, '_blank', 'noopener');
    else link?.click();
  }
});

document.addEventListener('keydown', event => {
  if (event.key === '/' && document.activeElement !== search) {
    event.preventDefault();
    search.focus();
  }
});

fetch('/_dropserve/api/apps')
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
