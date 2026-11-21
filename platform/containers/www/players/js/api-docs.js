// Renders /api/openapi.json as a readable reference. Deliberately dependency-free: the spec is JSON
// and the page only needs tables, so bundling Swagger UI or Redoc (megabytes, plus their own CDN
// expectations) would cost more than it explains.

const state = {
  spec: null,
  filter: '',
  open: new Set(),
  token: '',
};

function el(tag, cls, text) {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

// The spec writes inline code with backticks (README style). Escape first, then apply the two marks
// we actually use, so a field description cannot inject markup.
function richText(text) {
  const safe = String(text)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  const span = document.createElement('span');
  span.innerHTML = safe;
  return span;
}

function fmtType(schema) {
  if (!schema) return '—';
  if (schema.$ref) return schema.$ref.split('/').pop();
  if (schema.oneOf) return schema.oneOf.map(fmtType).join(' | ');
  if (schema.type === 'array') return 'array<' + fmtType(schema.items) + '>';
  return schema.type || '—';
}

// Flatten a schema into rows so nested objects can be read without clicking through a tree.
function flatten(schema, prefix, rows, depth) {
  if (!schema || depth > 4) return;
  const type = fmtType(schema);
  rows.push({ name: prefix || '(root)', type: type, desc: schema.description || '' });

  const props = schema.properties || (schema.allOf || []).reduce((acc, s) => Object.assign(acc, s.properties || {}), {});
  for (const [key, sub] of Object.entries(props)) {
    flatten(sub, prefix ? prefix + '.' + key : key, rows, depth + 1);
  }
  if (schema.type === 'array' && schema.items) {
    flatten(schema.items, prefix + '[]', rows, depth + 1);
  }
}

function typeTable(rows) {
  const table = el('table', 'api-table');
  const head = el('tr');
  ['名稱', '型別', '說明'].forEach((h) => head.appendChild(el('th', '', h)));
  table.appendChild(head);
  rows.forEach((r) => {
    const tr = el('tr');
    const name = el('td');
    name.appendChild(el('code', '', r.name));
    tr.appendChild(name);
    tr.appendChild(el('td', '', r.type));
    const descCell = el('td');
    descCell.appendChild(richText(r.desc || ''));
    tr.appendChild(descCell);
    table.appendChild(tr);
  });
  return table;
}

function curlFor(method, path, body) {
  const token = state.token || '<API token>';
  const lines = [
    `curl -sS -X ${method.toUpperCase()} 'http://localhost:882${path}' \\`,
    `  -H 'Content-Type: application/json' \\`,
    `  -d '${JSON.stringify(Object.assign({ token: token }, body))}'`,
  ];
  return lines.join('\n');
}

function renderEndpoint(path, op) {
  const wrap = el('div', 'api-ep');
  const head = el('div', 'api-ep-head');
  head.appendChild(el('span', 'api-method', 'POST'));
  head.appendChild(el('span', 'api-path', path));
  head.appendChild(el('span', 'api-summary', op.summary || ''));

  const body = el('div', 'api-ep-body');
  body.classList.add('hidden');
  if (state.open.has(path)) body.classList.remove('hidden');

  head.addEventListener('click', () => {
    if (state.open.has(path)) {
      state.open.delete(path);
      body.classList.add('hidden');
    } else {
      state.open.add(path);
      body.classList.remove('hidden');
    }
  });

  if (op.description) {
    const p = el('p', 'fs-13');
    p.appendChild(richText(op.description));
    body.appendChild(p);
  }

  if (op['x-actions']) {
    body.appendChild(el('div', 'api-section-title', 'action'));
    const table = el('table', 'api-table');
    const head2 = el('tr');
    ['action', '行為'].forEach((h) => head2.appendChild(el('th', '', h)));
    table.appendChild(head2);
    op['x-actions'].forEach((a) => {
      const tr = el('tr');
      const c1 = el('td');
      c1.appendChild(el('code', '', a.action || '（省略）'));
      tr.appendChild(c1);
      const actionCell = el('td');
      actionCell.appendChild(richText(a.description || ''));
      tr.appendChild(actionCell);
      table.appendChild(tr);
    });
    body.appendChild(table);
  }

  const reqSchema = op.requestBody && op.requestBody.content['application/json'] &&
    op.requestBody.content['application/json'].schema;
  if (reqSchema) {
    body.appendChild(el('div', 'api-section-title', '請求欄位'));
    const rows = [];
    flatten(reqSchema, '', rows, 0);
    body.appendChild(typeTable(rows.filter((r) => r.name !== '(root)')));
  }

  const ok = op.responses && op.responses['200'];
  const respSchema = ok && ok.content && ok.content['application/json'] &&
    ok.content['application/json'].schema;
  if (respSchema) {
    body.appendChild(el('div', 'api-section-title', '回應欄位'));
    const rows = [];
    const dataPart = (respSchema.allOf || []).find((s) => s.properties && s.properties.data);
    flatten(dataPart ? dataPart.properties.data : respSchema, 'data', rows, 0);
    body.appendChild(typeTable(rows));
  }

  const example = ok && ok.content && ok.content['application/json'] && ok.content['application/json'].example;
  if (example) {
    body.appendChild(el('div', 'api-section-title', '回應範例'));
    body.appendChild(el('pre', 'api-pre', JSON.stringify(example, null, 2)));
  }

  body.appendChild(el('div', 'api-section-title', 'curl'));
  const reqExample = op.requestBody && op.requestBody.content['application/json'] &&
    op.requestBody.content['application/json'].example;
  const bodyForCurl = {};
  if (reqExample && reqExample.action && reqExample.action !== '（省略）') bodyForCurl.action = reqExample.action;
  body.appendChild(el('pre', 'api-pre', curlFor('post', path, bodyForCurl)));

  wrap.appendChild(head);
  wrap.appendChild(body);
  return wrap;
}

function render() {
  const groups = document.getElementById('apiGroups');
  groups.innerHTML = '';
  if (!state.spec) return;

  const tagOf = {};
  (state.spec.tags || []).forEach((t) => { tagOf[t.name] = t.description || ''; });

  const byTag = new Map();
  Object.entries(state.spec.paths).forEach(([path, item]) => {
    const op = item.post || item.get;
    if (!op) return;
    const hay = (path + ' ' + (op.summary || '') + ' ' + (op.description || '')).toLowerCase();
    if (state.filter && !hay.includes(state.filter)) return;
    const tag = (op.tags && op.tags[0]) || 'other';
    if (!byTag.has(tag)) byTag.set(tag, []);
    byTag.get(tag).push([path, op]);
  });

  let shown = 0;
  byTag.forEach((eps, tag) => {
    groups.appendChild(el('h2', 'api-group-title', tag + '（' + eps.length + '）'));
    if (tagOf[tag]) groups.appendChild(el('p', 'text-dim fs-13', tagOf[tag]));
    eps.sort((a, b) => a[0].localeCompare(b[0]));
    eps.forEach(([path, op]) => {
      groups.appendChild(renderEndpoint(path, op));
      shown++;
    });
  });

  const count = document.getElementById('apiCount');
  count.textContent = shown + ' / ' + Object.keys(state.spec.paths).length + ' 個端點';
}

async function load() {
  const errBox = document.getElementById('apiError');
  try {
    const res = await fetch('/openapi.json', { cache: 'no-cache' });
    if (!res.ok) throw new Error('HTTP ' + res.status);
    state.spec = await res.json();
    document.getElementById('apiTitle').textContent = state.spec.info.title;
    document.getElementById('apiVersion').textContent =
      'v' + state.spec.info.version + ' · OpenAPI ' + state.spec.openapi;
    document.getElementById('apiDesc').textContent = state.spec.info.description || '';
    render();
  } catch (e) {
    errBox.classList.remove('hidden');
    errBox.textContent = '無法載入 /openapi.json：' + e.message;
  }
}

function bindToken() {
  const input = document.getElementById('tokenInput');
  const eye = document.getElementById('tokenEye');
  input.addEventListener('input', () => {
    state.token = input.value.trim();
    render();
  });
  eye.addEventListener('click', () => {
    const hidden = input.type === 'password';
    input.type = hidden ? 'text' : 'password';
    eye.textContent = hidden ? '隱藏' : '顯示';
  });
}

function bindFilter() {
  const input = document.getElementById('apiFilter');
  input.addEventListener('input', () => {
    state.filter = input.value.trim().toLowerCase();
    render();
  });
}

bindToken();
bindFilter();
load();
