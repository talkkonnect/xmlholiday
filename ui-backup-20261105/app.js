'use strict';
// FreeSWITCH Holiday Calendar UI — vanilla JS, no external dependencies (works offline).

const $ = (id) => document.getElementById(id);
function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === undefined || v === null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (k === 'checked' || k === 'value' || k === 'disabled' || k === 'selected') el[k] = v;
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const c of kids.flat(Infinity)) if (c !== null && c !== undefined && c !== false) el.append(c.nodeType ? c : String(c));
  return el;
}
async function api(method, url, body) {
  const opt = { method, headers: {} };
  if (body !== undefined) { opt.headers['Content-Type'] = 'application/json'; opt.body = JSON.stringify(body); }
  const r = await fetch(url, opt);
  const t = await r.text();
  let j; try { j = t ? JSON.parse(t) : {}; } catch { j = { error: t }; }
  if (!r.ok) { const e = new Error(j.error || r.statusText); e.status = r.status; e.body = j; throw e; }
  return j;
}
function msg(el, text, kind) {
  el.textContent = text; el.className = 'msg' + (kind ? ' ' + kind : ''); el.classList.toggle('hidden', !text);
}
const DOW = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
const TH_MONTHS = ['มกราคม', 'กุมภาพันธ์', 'มีนาคม', 'เมษายน', 'พฤษภาคม', 'มิถุนายน', 'กรกฎาคม', 'สิงหาคม', 'กันยายน', 'ตุลาคม', 'พฤศจิกายน', 'ธันวาคม'];
// Thai names for Thai holidays — UI display only. Stored labels (and therefore
// the generated XML) stay in English. Ordered most specific first.
const TH_NAMES = [
  [/bhumibol.*(birthday|father)|father'?s day|national day/, 'วันคล้ายวันพระบรมราชสมภพ พระบาทสมเด็จพระบรมชนกาธิเบศร มหาภูมิพลอดุลยเดชมหาราช บรมนาถบพิตร วันชาติ และวันพ่อแห่งชาติ'],
  [/bhumibol/, 'วันนวมินทรมหาราช'],
  [/chulalongkorn/, 'วันปิยมหาราช'],
  [/sirikit.*anniversary|queen mother.*anniversary/, 'วันคล้ายวันพระราชสมภพ สมเด็จพระบรมราชชนนีพันปีหลวง และวันแม่แห่งชาติ'],
  [/sirikit|queen mother|mother'?s day/, 'วันเฉลิมพระชนมพรรษา สมเด็จพระบรมราชชนนีพันปีหลวง และวันแม่แห่งชาติ'],
  [/vajiralongkorn|king'?s birthday/, 'วันเฉลิมพระชนมพรรษา พระบาทสมเด็จพระวชิรเกล้าเจ้าอยู่หัว'],
  [/suthida|queen'?s birthday/, 'วันเฉลิมพระชนมพรรษา สมเด็จพระนางเจ้าสุทิดา พัชรสุธาพิมลลักษณ พระบรมราชินี'],
  [/chakri/, 'วันจักรี'],
  [/songkran/, 'วันสงกรานต์'],
  [/labou?r day/, 'วันแรงงานแห่งชาติ'],
  [/coronation/, 'วันฉัตรมงคล'],
  [/visakha|vesak|wisakha/, 'วันวิสาขบูชา'],
  [/makha|magha/, 'วันมาฆบูชา'],
  [/asa\w*ha\b|asalha|asahna/, 'วันอาสาฬหบูชา'],
  [/end of (the )?buddhist lent|ok phansa|awk phansa/, 'วันออกพรรษา'],
  [/buddhist lent|khao phansa/, 'วันเข้าพรรษา'],
  [/constitution/, 'วันรัฐธรรมนูญ'],
  [/new year'?s eve/, 'วันสิ้นปี'],
  [/chinese new year|lunar new year/, 'วันตรุษจีน'],
  [/new year/, 'วันขึ้นปีใหม่'],
  [/loy krathong/, 'วันลอยกระทง'],
  [/children'?s day/, 'วันเด็กแห่งชาติ'],
  [/teacher'?s day/, 'วันครู'],
  [/plou?ghing|plowing/, 'วันพืชมงคล'],
  [/mid-?year/, 'วันหยุดภาคครึ่งปีของสถาบันการเงิน'],
  [/special (public )?holiday|bridge (public )?holiday/, 'วันหยุดพิเศษ'],
];
const SUBST_RE = /^(?:substitution (?:for|of)|substitute (?:for|holiday for)|day off for|compensatory (?:holiday )?for)\s+(.*)$/;
function thaiName(label) {
  let l = String(label || '').toLowerCase().replace(/[’‘`]/g, "'").replace(/\s+/g, ' ').trim();
  if (!l) return '';
  let prefix = '';
  const m = l.match(SUBST_RE);
  if (m) { prefix = 'ชดเชย'; l = m[1].replace(/\s*\([^)]*\)\s*$/, ''); }
  else if (/\(observed\)|\bobserved\b/.test(l)) prefix = 'ชดเชย';
  for (const [re, th] of TH_NAMES) if (re.test(l)) return prefix + th;
  return '';
}
// "New Year's Day" -> "New Year's Day (วันขึ้นปีใหม่)". Labels that already
// contain Thai, or are not recognised, are shown unchanged.
function displayLabel(label) {
  if (!label || /[฀-๿]/.test(label)) return label || '';
  const th = thaiName(label);
  return th ? `${label} (${th})` : label;
}
const pad = (n) => String(n).padStart(2, '0');
const iso = (y, m, d) => `${y}-${pad(m)}-${pad(d)}`;
function dowOf(s) { const [y, m, d] = s.split('-').map(Number); return new Date(Date.UTC(y, m - 1, d)).getUTCDay(); }
function validISO(s, year) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(s)) return false;
  const [y, m, d] = s.split('-').map(Number);
  const t = new Date(Date.UTC(y, m - 1, d));
  return t.getUTCFullYear() === y && t.getUTCMonth() === m - 1 && t.getUTCDate() === d && (!year || y === year);
}
function validVar(v) {
  if (!v) return 'variable name is required';
  if (v.trim() !== v) return 'no leading/trailing spaces';
  if (/[\s=\/\x00-\x1f]/.test(v)) return "no spaces, '=' or '/'";
  if (v.length > 255) return 'too long';
  return '';
}

// ======================================================================
// Screen switching with unsaved-change protection
// ======================================================================
let currentTab = 'a';
document.querySelectorAll('nav .tab').forEach((b) => b.addEventListener('click', async () => {
  const tab = b.dataset.tab;
  if (tab === currentTab) return;
  if (currentTab === 'a' && A.dirty() && !confirm('Screen A has unsaved changes. Discard them?')) return;
  if (currentTab === 'b' && B.dirty && !confirm('The calendar has unsaved changes. Discard them?')) return;
  if (currentTab === 'a' && A.dirty()) A.revert();
  if (currentTab === 'b') B.dirty = false;
  currentTab = tab;
  document.querySelectorAll('nav .tab').forEach((x) => x.classList.toggle('active', x === b));
  $('screenA').classList.toggle('hidden', tab !== 'a');
  $('screenB').classList.toggle('hidden', tab !== 'b');
  if (tab === 'a') await A.load(); else await B.enter();
}));
window.addEventListener('beforeunload', (e) => { if (A.dirty() || B.dirty) { e.preventDefault(); e.returnValue = ''; } });

// ======================================================================
// SCREEN A — Companies & Variables
// ======================================================================
const A = {
  orig: null, draft: null, nextId: -1,
  async load() {
    try {
      this.orig = await api('GET', '/api/registry');
      this.revert();
    } catch (e) { msg($('aMsg'), 'Load failed: ' + e.message, 'error'); }
  },
  revert() {
    this.draft = JSON.parse(JSON.stringify(this.orig));
    msg($('aMsg'), '');
    this.render();
  },
  key(x) { return JSON.stringify(x); },
  changes() {
    const by = (arr) => Object.fromEntries(arr.map((x) => [x.id, this.key(x)]));
    const o = { c: by(this.orig.companies), b: by(this.orig.branches), a: by(this.orig.assignments) };
    return {
      companies: this.draft.companies.filter((x) => o.c[x.id] !== this.key(x)),
      branches: this.draft.branches.filter((x) => o.b[x.id] !== this.key(x)),
      assignments: this.draft.assignments.filter((x) => o.a[x.id] !== this.key(x)),
    };
  },
  dirty() {
    if (!this.draft) return false;
    const c = this.changes();
    return c.companies.length + c.branches.length + c.assignments.length > 0;
  },
  varUsers(v) { return this.draft.assignments.filter((a) => a.variable_name === v); },
  origVarUsers(v) { return this.orig.assignments.filter((a) => a.variable_name === v); },
  label(a) {
    const c = this.draft.companies.find((x) => x.id === a.company_id);
    const b = a.branch_id ? this.draft.branches.find((x) => x.id === a.branch_id) : null;
    return (c ? c.name : '?') + (b ? ' / ' + b.name : ' (company)');
  },
  touch() { $('aDirty').classList.toggle('hidden', !this.dirty()); },
  render() {
    const body = $('aBody'); body.replaceChildren();
    const showInactive = $('aShowInactive').checked;
    const assignRows = (company, branch) => this.draft.assignments
      .filter((a) => a.company_id === company.id && (branch ? a.branch_id === branch.id : !a.branch_id))
      .filter((a) => showInactive || a.active)
      .map((a) => this.assignRow(a, !!branch));
    for (const c of this.draft.companies) {
      if (!showInactive && !c.active) continue;
      const branches = this.draft.branches.filter((b) => b.company_id === c.id);
      body.append(h('tr', { class: 'company' + (c.active ? '' : ' inactive') + (c.id < 0 ? ' new' : '') },
        h('td', {}, h('input', { value: c.name, placeholder: 'Company name', class: c.name.trim() ? '' : 'invalid',
          oninput: (e) => { c.name = e.target.value; e.target.classList.toggle('invalid', !c.name.trim()); this.touch(); } })),
        h('td', { class: 'hint' }, branches.length ? `${branches.length} branch(es)` : 'no branches — register the company variable below'),
        h('td', {}, h('input', { type: 'checkbox', checked: c.active, title: 'uncheck to deactivate', onchange: (e) => { c.active = e.target.checked; this.render(); this.touch(); } })),
        h('td', {}),
        h('td', {},
          h('button', { class: 'small', onclick: () => this.addBranch(c) }, '+ Branch'), ' ',
          h('button', { class: 'small', onclick: () => this.addAssign(c, null) }, '+ Variable'))));
      body.append(...assignRows(c, null));
      for (const b of branches) {
        if (!showInactive && !b.active) continue;
        body.append(h('tr', { class: 'branch' + (b.active && c.active ? '' : ' inactive') + (b.id < 0 ? ' new' : '') },
          h('td', {}, '↳ ', h('input', { value: b.name, placeholder: 'Branch name', class: b.name.trim() ? '' : 'invalid',
            oninput: (e) => { b.name = e.target.value; e.target.classList.toggle('invalid', !b.name.trim()); this.touch(); } })),
          h('td', { class: 'hint' }, ''),
          h('td', {}, h('input', { type: 'checkbox', checked: b.active, onchange: (e) => { b.active = e.target.checked; this.render(); this.touch(); } })),
          h('td', {}),
          h('td', {}, h('button', { class: 'small', onclick: () => this.addAssign(c, b) }, '+ Variable'))));
        body.append(...assignRows(c, b));
      }
    }
    if (!this.draft.companies.length) body.append(h('tr', {}, h('td', { colspan: 5, class: 'hint' }, 'No companies yet. Click “+ Company”.')));
    this.touch();
  },
  assignRow(a, ofBranch) {
    const users = this.varUsers(a.variable_name);
    const shared = a.variable_name && users.length > 1;
    const err = validVar(a.variable_name);
    const input = h('input', { class: 'var' + (err ? ' invalid' : ''), value: a.variable_name, placeholder: 'e.g. jwd-working-time', title: err || '',
      onchange: (e) => {
        const nv = e.target.value;
        const origA = this.orig.assignments.find((x) => x.id === a.id);
        if (origA && origA.variable_name !== nv && this.origVarUsers(origA.variable_name).length > 1) {
          const others = this.origVarUsers(origA.variable_name).filter((x) => x.id !== a.id).map((x) => this.label(x)).join('\n  ');
          if (!confirm(`“${origA.variable_name}” is a SHARED calendar, also used by:\n  ${others}\n\nChanging this assignment detaches it from that shared calendar. Continue?`)) {
            e.target.value = a.variable_name; return;
          }
        }
        if (nv && nv !== a.variable_name && this.varUsers(nv).length > 0) {
          const others = this.varUsers(nv).map((x) => this.label(x)).join('\n  ');
          if (!confirm(`“${nv}” is already used by:\n  ${others}\n\nThis assignment will SHARE that calendar. Continue?`)) { e.target.value = a.variable_name; return; }
        }
        a.variable_name = nv; this.render();
      } });
    return h('tr', { class: 'assign' + (ofBranch ? ' ofbranch' : '') + (a.active ? '' : ' inactive') + (a.id < 0 ? ' new' : '') },
      h('td', {}, ofBranch ? 'branch variable' : 'company-level variable'),
      h('td', {}, input, err ? h('div', { class: 'warn' }, err) : null),
      h('td', {}, h('input', { type: 'checkbox', checked: a.active, onchange: (e) => { a.active = e.target.checked; this.render(); } })),
      h('td', {}, shared
        ? h('span', {}, h('span', { class: 'badge b-shared' }, `Shared calendar ×${users.length}`),
            h('div', { class: 'sharelist' }, users.filter((x) => x !== a).map((x) => '↔ ' + this.label(x)).join('; ')))
        : h('span', { class: 'hint' }, 'own calendar')),
      h('td', {}));
  },
  addCompany() {
    this.draft.companies.push({ id: this.nextId--, name: '', active: true }); this.render();
    const inputs = $('aBody').querySelectorAll('tr.company input:not([type])'); inputs[inputs.length - 1]?.focus();
  },
  addBranch(c) { this.draft.branches.push({ id: this.nextId--, company_id: c.id, name: '', active: true }); this.render(); },
  addAssign(c, b) {
    if (!b && this.draft.branches.some((x) => x.company_id === c.id) &&
      !confirm('This company has branches. A company-level variable is only needed if you explicitly want one. Add it anyway?')) return;
    this.draft.assignments.push({ id: this.nextId--, company_id: c.id, branch_id: b ? b.id : null, variable_name: '', active: true }); this.render();
  },
  async save() {
    const ch = this.changes();
    const problems = [];
    ch.companies.forEach((c) => { if (!c.name.trim()) problems.push('A company name is empty.'); });
    ch.branches.forEach((b) => { if (!b.name.trim()) problems.push('A branch name is empty.'); });
    ch.assignments.forEach((a) => { const e = validVar(a.variable_name); if (e) problems.push(`${this.label(a)}: ${e}`); });
    if (problems.length) { msg($('aMsg'), 'Cannot save:\n' + [...new Set(problems)].join('\n'), 'error'); return; }
    if (!ch.companies.length && !ch.branches.length && !ch.assignments.length) { msg($('aMsg'), 'Nothing to save.'); return; }
    try {
      const r = await api('POST', '/api/registry', ch);
      this.orig = r.registry; this.revert();
      msg($('aMsg'), 'Saved.', 'ok');
    } catch (e) { msg($('aMsg'), 'Save failed: ' + e.message, 'error'); }
  },
};
$('aAddCompany').onclick = () => A.addCompany();
$('aSave').onclick = () => A.save();
$('aRevert').onclick = () => { if (!A.dirty() || confirm('Discard unsaved changes?')) A.revert(); };
$('aShowInactive').onchange = () => A.render();

// ======================================================================
// SCREEN B — Holiday Calendar & Generator
// ======================================================================
const TEMPLATE = '__template__';
const B = {
  year: new Date().getFullYear() + 1,
  vars: [], reg: null,
  target: null,            // {kind:'template'} | {kind:'variable', variable}
  items: new Map(),        // date -> item
  version: 0, exists: false,
  hl: new Set(),
  dirty: false,
  sel: { company: '', branch: '', variable: '' }, // committed selector state (for revert on cancel)

  setDirty(v) { this.dirty = v; $('bDirty').classList.toggle('hidden', !v); },
  guard() { return !this.dirty || confirm('You have unsaved calendar changes. Discard them?'); },

  async enter() {
    try {
      const [reg, vars, cfg] = await Promise.all([api('GET', '/api/registry'), api('GET', '/api/variables'), api('GET', '/api/settings')]);
      this.reg = reg; this.vars = vars;
      if (!this.yearInit && cfg.default_year) this.year = Number(cfg.default_year);
      this.yearInit = true;
      $('bYear').value = this.year;
      this.fillCompanies();
      await this.selectionChanged(true);
    } catch (e) { msg($('bNotice'), 'Load failed: ' + e.message, 'error'); }
  },

  // ---------- selectors ----------
  fillCompanies() {
    const sel = $('bCompany'); sel.replaceChildren();
    sel.append(h('option', { value: TEMPLATE }, `★ Standard template (${this.year})`));
    for (const c of this.reg.companies) {
      const hasAssign = this.reg.assignments.some((a) => a.company_id === c.id);
      sel.append(h('option', { value: c.id, disabled: !hasAssign }, c.name + (c.active ? '' : ' (inactive)') + (hasAssign ? '' : ' — no variable')));
    }
    if (![...sel.options].some((o) => o.value === String(this.sel.company))) this.sel.company = TEMPLATE;
    sel.value = this.sel.company;
    this.fillBranches();
  },
  fillBranches() {
    const cid = $('bCompany').value;
    const bsel = $('bBranch'); bsel.replaceChildren();
    if (cid === TEMPLATE) { $('bBranchWrap').classList.add('hidden'); $('bVarWrap').classList.add('hidden'); return; }
    const c = Number(cid);
    const assigns = this.reg.assignments.filter((a) => a.company_id === c);
    const branches = this.reg.branches.filter((b) => b.company_id === c && assigns.some((a) => a.branch_id === b.id));
    if (assigns.some((a) => !a.branch_id)) bsel.append(h('option', { value: '' }, '(company level)'));
    for (const b of branches) bsel.append(h('option', { value: b.id }, b.name + (b.active ? '' : ' (inactive)')));
    $('bBranchWrap').classList.toggle('hidden', branches.length === 0);
    if ([...bsel.options].some((o) => o.value === String(this.sel.branch))) bsel.value = this.sel.branch;
    this.fillVariables();
  },
  fillVariables() {
    const c = Number($('bCompany').value);
    const bid = $('bBranch').value ? Number($('bBranch').value) : null;
    const vsel = $('bVariable'); vsel.replaceChildren();
    const assigns = this.reg.assignments.filter((a) => a.company_id === c && (a.branch_id || null) === bid);
    for (const a of assigns) vsel.append(h('option', { value: a.variable_name }, a.variable_name + (a.active ? '' : ' (inactive)')));
    $('bVarWrap').classList.toggle('hidden', assigns.length <= 1);
    if ([...vsel.options].some((o) => o.value === this.sel.variable)) vsel.value = this.sel.variable;
  },
  currentSelection() {
    const company = $('bCompany').value;
    if (company === TEMPLATE) return { company, branch: '', variable: '' };
    return { company, branch: $('bBranch').value, variable: $('bVariable').value };
  },
  restoreSelectors() {
    $('bYear').value = this.year;
    this.fillCompanies();
  },
  async selectionChanged(force) {
    const s = this.currentSelection();
    if (!force && s.company === this.sel.company && s.branch === this.sel.branch && s.variable === this.sel.variable && Number($('bYear').value) === this.year) return;
    if (!force && !this.guard()) { this.restoreSelectors(); return; }
    this.sel = s;
    this.year = Number($('bYear').value) || this.year;
    $('bCompany').options[0].textContent = `★ Standard template (${this.year})`;
    this.target = s.company === TEMPLATE ? { kind: 'template' } : { kind: 'variable', variable: s.variable };
    $('bSugg').classList.add('hidden');
    $('bXmlWrap').classList.add('hidden');
    await this.loadTarget();
  },

  // ---------- load ----------
  add(item, overwrite) {
    if (!overwrite && this.items.has(item.date)) return false;
    this.items.set(item.date, Object.assign({ label: '', origin: 'manual', selected: false, persist: false }, item));
    return true;
  },
  async loadTarget() {
    this.items = new Map(); this.hl = new Set(); this.version = 0; this.exists = false;
    msg($('bNotice'), '');
    $('bShared').classList.add('hidden');
    $('bBulk').classList.toggle('hidden', this.target.kind !== 'template');
    try {
      if (this.target.kind === 'template') {
        const t = await api('GET', `/api/templates/${this.year}`);
        this.exists = t.exists;
        t.dates.forEach((d) => this.add({ ...d, selected: true, persist: true }));
        $('bVarDisplay').textContent = `standard template ${this.year}`;
        msg($('bNotice'), t.exists
          ? `Approved standard template for ${this.year} (approved ${t.approved_at}). Saving it does NOT change existing variable calendars; new variables start from a copy. Use “Apply to variables…” to push holidays to existing calendars.`
          : `No standard template for ${this.year} yet. Load Thai holiday suggestions, review them, move the ones you approve to the right and Save to create the template.`);
      } else {
        const v = this.target.variable;
        $('bVarDisplay').textContent = v || '—';
        if (!v) { msg($('bNotice'), 'Select a company/branch with a registered variable (Screen A).', 'error'); this.render(); return; }
        const r = await api('GET', `/api/calendars/${this.year}/${encodeURIComponent(v)}`);
        if (r.shared) {
          const el = $('bShared'); el.classList.remove('hidden');
          el.textContent = `⚠ Shared calendar: “${v}” is used by ${r.assignments.length} assignments — editing here changes ALL of them: ` +
            r.assignments.map((a) => a.company_name + (a.branch_name ? ' / ' + a.branch_name : '') + (a.active ? '' : ' (inactive)')).join(' · ');
        }
        if (r.exists) {
          this.exists = true; this.version = r.calendar.version;
          r.calendar.dates.forEach((d) => this.add({ ...d, persist: true }));
          r.template_dates.forEach((d) => this.add({ ...d, origin: 'template', selected: false }));
        } else if (r.template_exists) {
          r.template_dates.forEach((d) => this.add({ ...d, origin: 'template', selected: true, persist: true }));
          msg($('bNotice'), `No saved calendar for ${v} in ${this.year}. It has been initialised as a copy of the ${this.year} standard template — review and Save to keep it.`);
          this.setDirty(true); this.render(); return;
        } else {
          msg($('bNotice'), `No saved calendar for ${v} in ${this.year} and no standard template for ${this.year}. Load suggestions, Copy From another calendar, or add custom dates.`);
        }
      }
    } catch (e) { msg($('bNotice'), 'Load failed: ' + e.message, 'error'); }
    this.setDirty(false);
    this.render();
  },

  // ---------- suggestions ----------
  async loadSuggestions() {
    const box = $('bSugg'); box.classList.remove('hidden'); box.replaceChildren('Retrieving Thai holiday suggestions…');
    let r;
    try { r = await api('GET', `/api/holiday-suggestions?year=${this.year}`); }
    catch (e) { box.replaceChildren(h('div', { class: 'err' }, 'Suggestion retrieval failed: ' + e.message + '. You can still add dates manually and work with saved calendars.')); return; }
    const added = r.suggestions.filter((s) => this.add({ ...s, origin: 'suggested', selected: false })).length;
    const counts = {}; r.suggestions.forEach((s) => { counts[s.verification_status] = (counts[s.verification_status] || 0) + 1; });
    const sources = [...new Map(r.suggestions.map((s) => [s.source_name, s])).values()];
    box.replaceChildren(...[
      h('div', {}, h('b', {}, `${r.suggestions.length} suggestion(s) for ${this.year}`),
        r.from_cache ? h('span', { class: 'err' }, ' — from offline cache (live retrieval failed or unavailable)') : '',
        ` · ${added} new added to Available · `, Object.entries(counts).map(([k, n]) => h('span', { class: 'badge b-' + k }, `${n} ${k}`)).map((x) => [' ', x])),
      sources.map((s) => h('div', { class: 'hint' }, 'Source: ', h('a', { href: s.source_url, target: '_blank', rel: 'noopener' }, s.source_name),
        ` — retrieved ${new Date(s.retrieved_at).toLocaleString()}`)),
      (r.errors || []).map((e) => h('div', { class: 'err' }, `✖ ${e.provider}: ${e.error}`)),
      r.suggestions.length === 0 ? h('div', { class: 'err' }, 'No suggestions available. Nothing has been invented — add holidays manually with the custom-date picker or by clicking the calendar.') : '',
      h('div', { class: 'hint' }, 'Suggestions are NOT selected automatically. Review them and move approved dates to the right. Unverified dates are not official.')].flat());
    this.render();
  },

  // ---------- edit ops ----------
  move(dates, selected) {
    let n = 0;
    for (const d of dates) {
      const it = this.items.get(d);
      if (it && it.selected !== selected) { it.selected = selected; it.persist = true; n++; }
    }
    if (n) { this.hl.clear(); this.setDirty(true); this.render(); }
  },
  visible(list) {
    const f = (list === 'left' ? $('bFilterLeft') : $('bFilterRight')).value.toLowerCase();
    return [...this.items.values()].filter((i) => i.selected === (list === 'right'))
      .filter((i) => !f || (i.date + ' ' + displayLabel(i.label) + ' ' + (i.source_name || '')).toLowerCase().includes(f))
      .sort((a, b) => a.date.localeCompare(b.date));
  },
  addCustom(date, label) {
    if (!validISO(date, this.year)) { alert(`Invalid date. Pick a real date in ${this.year}.`); return false; }
    const ex = this.items.get(date);
    if (ex) { alert(`${date} is already listed (${displayLabel(ex.label) || 'no label'}) in ${ex.selected ? 'Selected' : 'Available'}.`); this.hl = new Set([date]); this.render(); return false; }
    this.add({ date, label: (label || '').trim() || 'Custom holiday', origin: 'manual', selected: false, persist: true });
    this.hl = new Set([date]);
    this.setDirty(true); this.render(); return true;
  },

  deleteHighlighted() {
    const its = [...this.hl].map((d) => this.items.get(d)).filter((i) => i && !i.selected).sort((a, b) => a.date.localeCompare(b.date));
    if (!its.length) { alert('Highlight one or more holidays in the Available list first. (Move selected holidays back to Available before deleting them.)'); return; }
    const list = its.map((i) => `${i.date}  ${displayLabel(i.label) || '(no label)'}`).join('\n');
    const reappear = its.some((i) => i.origin === 'template' || i.origin === 'suggested')
      ? '\n\nNote: standard-template / suggested dates may be offered again when the calendar is reloaded or suggestions are loaded.' : '';
    if (!confirm(`Delete ${its.length} holiday(s) from this calendar?\n\n${list}${reappear}\n\nThe deletion becomes permanent when you Save.`)) return;
    its.forEach((i) => { this.items.delete(i.date); this.hl.delete(i.date); });
    this.setDirty(true); this.render();
  },

  // ---------- render ----------
  render() {
    const li = (it) => {
      const cls = (this.hl.has(it.date) ? 'hl ' : '') + (it.origin === 'manual' || it.origin === 'imported' ? 'custom' : '');
      return h('li', { class: cls, 'data-date': it.date, onclick: (e) => this.clickItem(it.date, e) },
        h('span', { class: 'd' }, `${it.date} ${DOW[dowOf(it.date)]}`),
        h('span', { class: 'l' }, displayLabel(it.label) || '(no label)'),
        h('span', { class: 'meta' },
          h('span', { class: 'badge b-' + it.origin }, it.origin),
          it.verification_status ? h('span', { class: 'badge b-' + it.verification_status }, it.verification_status) : '',
          it.category ? it.category : '',
          it.source_url ? h('a', { href: it.source_url, target: '_blank', rel: 'noopener', onclick: (e) => e.stopPropagation() }, it.source_name || 'source') : (it.source_name || '')));
    };
    const left = this.visible('left'), right = this.visible('right');
    $('bAvail').replaceChildren(...left.map(li));
    $('bSelected').replaceChildren(...right.map(li));
    $('bAvailCount').textContent = `(${left.length})`;
    $('bSelCount').textContent = `(${right.length})`;
    $('bDeleteHl').disabled = ![...this.hl].some((d) => this.items.has(d) && !this.items.get(d).selected);
    this.renderCalendar();
  },
  lastClick: null,
  clickItem(date, e) {
    const it = this.items.get(date);
    if (e && e.shiftKey && this.lastClick) {
      const list = this.visible(it.selected ? 'right' : 'left').map((i) => i.date);
      const a = list.indexOf(this.lastClick), b = list.indexOf(date);
      if (a >= 0 && b >= 0) { list.slice(Math.min(a, b), Math.max(a, b) + 1).forEach((d) => this.hl.add(d)); this.render(); return; }
    }
    if (this.hl.has(date)) this.hl.delete(date); else this.hl.add(date);
    this.lastClick = date;
    this.render();
  },
  renderCalendar() {
    const wrap = $('bCalendar'); wrap.replaceChildren();
    const today = new Date(); const todayIso = iso(today.getFullYear(), today.getMonth() + 1, today.getDate());
    for (let m = 1; m <= 12; m++) {
      const grid = h('div', { class: 'grid' }, ...DOW.map((d) => h('div', { class: 'dow' }, d[0] + d[1])));
      const first = new Date(Date.UTC(this.year, m - 1, 1)).getUTCDay();
      const days = new Date(Date.UTC(this.year, m, 0)).getUTCDate(); // handles leap years
      for (let i = 0; i < first; i++) grid.append(h('div', {}));
      for (let d = 1; d <= days; d++) {
        const ds = iso(this.year, m, d); const it = this.items.get(ds); const w = (first + d - 1) % 7;
        let cls = 'day' + (w === 0 || w === 6 ? ' wknd' : '') + (ds === todayIso ? ' today' : '');
        if (it) cls += it.selected ? ' sel' : ' avail';
        if (it && (it.origin === 'manual' || it.origin === 'imported')) cls += ' custom';
        if (this.hl.has(ds)) cls += ' hl';
        grid.append(h('div', { class: cls, title: it ? `${displayLabel(it.label)} (${it.selected ? 'selected' : 'available'}, ${it.origin})` : 'click to add a custom holiday',
          onclick: () => this.clickDay(ds) }, d));
      }
      wrap.append(h('div', { class: 'month' }, h('h4', {}, `${MONTHS[m - 1]} (${TH_MONTHS[m - 1]}) ${this.year}`), grid));
    }
  },
  clickDay(ds) {
    if (this.items.has(ds)) {
      this.clickItem(ds);
      document.querySelector(`li[data-date="${ds}"]`)?.scrollIntoView({ block: 'nearest' });
      return;
    }
    const label = prompt(`Create a custom holiday on ${ds} (${DOW[dowOf(ds)]})?\nIt will be added to the Available list; move it right to select it.\n\nHoliday name:`, 'Custom holiday');
    if (label !== null) this.addCustom(ds, label);
  },

  // ---------- save ----------
  payload() {
    return [...this.items.values()].filter((i) => i.persist || i.selected).map((i) => ({
      date: i.date, label: i.label || '', origin: i.origin || 'manual', selected: !!i.selected, category: i.category || '',
      source_name: i.source_name || '', source_url: i.source_url || '', verification_status: i.verification_status || '',
    }));
  },
  async save() {
    try {
      if (this.target.kind === 'template') {
        const dates = this.payload().filter((d) => d.selected);
        const unverified = dates.filter((d) => d.verification_status && d.verification_status !== 'official').length;
        if (unverified && !confirm(`${unverified} selected date(s) are NOT from an official source (unverified/estimated). Approve them in the standard template anyway?`)) return;
        const r = await api('POST', `/api/templates/${this.year}`, { dates });
        this.setDirty(false);
        await this.loadTarget();
        let text = `Standard template for ${this.year} saved (${r.template.dates.length} dates).`;
        if (r.created_calendars?.length) text += `\nNew calendars created from the template for: ${r.created_calendars.join(', ')}.`;
        msg($('bNotice'), text, 'ok');
        if (r.added_dates?.length) {
          if (confirm(`${r.added_dates.length} date(s) are new compared with the previous template. Existing variable calendars were NOT changed.\n\nReview applying them to existing calendars now?`))
            Bulk.open(r.added_dates);
        }
      } else {
        const v = this.target.variable;
        const r = await api('PUT', `/api/calendars/${this.year}/${encodeURIComponent(v)}`, { version: this.exists ? this.version : 0, dates: this.payload() });
        this.setDirty(false);
        await this.loadTarget();
        msg($('bNotice'), `Saved calendar ${v} ${this.year}: ${r.dates.filter((d) => d.selected).length} selected date(s).`, 'ok');
      }
      this.summary();
    } catch (e) {
      msg($('bNotice'), 'Save failed: ' + e.message + (e.status === 409 ? '\nReload the calendar (re-select it) and re-apply your edits.' : ''), 'error');
    }
  },

  // ---------- XML ----------
  async ensureSavedFirst() {
    if (!this.dirty) return true;
    if (confirm('Preview/Download use SAVED data only. Save your current changes first?\n\nOK = save now, Cancel = continue with saved data')) {
      await this.save(); return !this.dirty;
    }
    return true;
  },
  async summary() {
    try {
      const r = await api('GET', `/api/xml/preview?year=${this.year}`);
      const s = r.summary;
      $('bSummary').replaceChildren('Saved data ', h('b', {}, String(this.year)), ': ',
        h('b', {}, String(s.unique_variables)), ' unique variables · ',
        h('b', {}, String(s.selected_dates)), ' selected dates · ',
        h('b', {}, String(s.xml_blocks)), ' XML blocks · ',
        h('b', {}, String(s.actions)), ' actions · break=', h('b', {}, s.break));
      return r;
    } catch (e) { $('bSummary').textContent = 'Summary unavailable: ' + e.message; return null; }
  },
  async preview() {
    if (!(await this.ensureSavedFirst())) return;
    const r = await this.summary(); if (!r) return;
    $('bXmlWrap').classList.remove('hidden');
    $('bXml').textContent = r.xml;
    $('bWarnings').replaceChildren(...r.summary.warnings.map((w) => h('div', {}, '⚠ ' + w)),
      r.summary.inactive_variables_skipped.length ? h('div', {}, 'Inactive variables skipped: ' + r.summary.inactive_variables_skipped.join(', ')) : '',
      r.summary.variables_without_dates.length ? h('div', {}, 'Active variables with no selected dates: ' + r.summary.variables_without_dates.join(', ')) : '');
  },
  async download() {
    if (!(await this.ensureSavedFirst())) return;
    const r = await this.summary(); if (!r) return;
    let url = `/api/xml/download?year=${this.year}`;
    if (r.summary.actions === 0) {
      if (!confirm(`WARNING: ${r.summary.warnings.join('\n')}\n\nDownload an empty <include> wrapper anyway?`)) return;
      url += '&confirm_empty=1';
    }
    const a = h('a', { href: url, download: r.summary.filename }); document.body.append(a); a.click(); a.remove();
  },
};

$('bCompany').onchange = () => { B.fillBranches(); B.selectionChanged(); };
$('bBranch').onchange = () => { B.fillVariables(); B.selectionChanged(); };
$('bVariable').onchange = () => B.selectionChanged();
$('bYear').onchange = () => {
  const y = Number($('bYear').value);
  if (!(y >= 1900 && y <= 9999)) { $('bYear').value = B.year; return; }
  B.selectionChanged();
};
$('bYearPrev').onclick = () => { $('bYear').value = B.year - 1; B.selectionChanged(); };
$('bYearNext').onclick = () => { $('bYear').value = B.year + 1; B.selectionChanged(); };
$('bLoadSugg').onclick = () => B.loadSuggestions();
$('bRight').onclick = () => B.move([...B.hl].filter((d) => !B.items.get(d)?.selected), true);
$('bLeft').onclick = () => B.move([...B.hl].filter((d) => B.items.get(d)?.selected), false);
$('bSelectAll').onclick = () => B.move(B.visible('left').map((i) => i.date), true);
$('bRemoveAll').onclick = () => { if (B.visible('right').length && confirm('Move ALL selected holidays back to Available?')) B.move(B.visible('right').map((i) => i.date), false); };
$('bHlAllLeft').onclick = () => { B.visible('left').forEach((i) => B.hl.add(i.date)); B.render(); };
$('bHlAllRight').onclick = () => { B.visible('right').forEach((i) => B.hl.add(i.date)); B.render(); };
$('bFilterLeft').oninput = () => B.render();
$('bFilterRight').oninput = () => B.render();
$('bAvail').ondblclick = (e) => { const d = e.target.closest('li')?.dataset.date; if (d) B.move([d], true); };
$('bSelected').ondblclick = (e) => { const d = e.target.closest('li')?.dataset.date; if (d) B.move([d], false); };
$('bDeleteHl').onclick = () => B.deleteHighlighted();
$('bAddCustom').onclick = () => { if (B.addCustom($('bCustomDate').value, $('bCustomLabel').value)) { $('bCustomLabel').value = ''; } };
$('bSave').onclick = () => B.save();
$('bPreview').onclick = () => B.preview();
$('bDownload').onclick = () => B.download();
$('bCopyFrom').onclick = () => Copy.open();
$('bBulk').onclick = () => {
  const dates = [...B.hl].map((d) => B.items.get(d)).filter((i) => i && i.selected);
  if (!dates.length) { alert('Highlight one or more SELECTED (right-hand) template holidays to apply to variable calendars.'); return; }
  if (B.dirty) { alert('Save the template first.'); return; }
  Bulk.open(dates);
};

// ======================================================================
// Copy From dialog
// ======================================================================
const Copy = {
  open() {
    const src = $('cSource'); src.replaceChildren(h('option', { value: TEMPLATE }, '★ Standard template'));
    for (const v of B.vars) src.append(h('option', { value: v.variable_name }, v.variable_name + (v.shared ? ` (shared ×${v.assignments.length})` : '')));
    $('cYear').value = B.year; msg($('cInfo'), 'Copied dates are added to the current (unsaved) calendar after confirmation. Nothing is saved until you click Save.');
    $('dlgCopy').showModal();
  },
  async run() {
    const src = $('cSource').value, y = Number($('cYear').value), mode = $('cMode').value;
    if (src !== TEMPLATE && B.target.kind === 'variable' && src === B.target.variable && y === B.year) { alert('Source and target are the same calendar.'); return; }
    let dates;
    try {
      if (src === TEMPLATE) {
        const t = await api('GET', `/api/templates/${y}`);
        if (!t.exists) { alert(`No standard template for ${y}.`); return; }
        dates = t.dates;
      } else {
        const r = await api('GET', `/api/calendars/${y}/${encodeURIComponent(src)}`);
        if (!r.exists) { alert(`${src} has no saved calendar for ${y}.`); return; }
        dates = r.calendar.dates.filter((d) => d.selected);
      }
    } catch (e) { alert('Copy failed: ' + e.message); return; }
    const skipped = [], lunar = [];
    const mapped = dates.map((d) => {
      if (y === B.year) return d;
      const nd = `${B.year}${d.date.slice(4)}`;
      if (!validISO(nd, B.year)) { skipped.push(d.date); return null; }
      if (d.category === 'buddhist-lunar' || d.category === 'substitute' || d.category === 'special') lunar.push(`${d.date} ${displayLabel(d.label)}`);
      return { ...d, date: nd };
    }).filter(Boolean);
    let text = `Copy ${mapped.length} date(s) from ${src === TEMPLATE ? 'standard template' : src} ${y} into ${B.target.kind === 'template' ? 'the standard template' : B.target.variable} ${B.year} (${mode === 'replace' ? 'REPLACING current selection' : 'adding to current selection'})?`;
    if (y !== B.year) text += `\n\nDifferent year: dates are mapped to the same day/month in ${B.year}.` +
      (lunar.length ? `\nWARNING — lunar/substitute/special holidays usually move between years:\n  ${lunar.join('\n  ')}` : '') +
      (skipped.length ? `\nSkipped (invalid in ${B.year}): ${skipped.join(', ')}` : '');
    if (!confirm(text)) return;
    $('dlgCopy').close();
    if (mode === 'replace') for (const it of B.items.values()) if (it.selected) { it.selected = false; it.persist = true; }
    for (const d of mapped) {
      const ex = B.items.get(d.date);
      if (ex) { ex.selected = true; ex.persist = true; }
      else B.add({ ...d, origin: 'copied', selected: true, persist: true });
    }
    B.setDirty(true); B.render();
    msg($('bNotice'), `Copied ${mapped.length} date(s) — not saved yet. Review and click Save.`);
  },
};
$('cLoad').onclick = (e) => { e.preventDefault(); Copy.run(); };

// ======================================================================
// Bulk apply dialog (template holidays -> existing variable calendars)
// ======================================================================
const Bulk = {
  dates: [],
  open(dates) {
    this.dates = dates.map((d) => ({ date: d.date, label: d.label, origin: 'template', category: d.category || '', source_name: d.source_name || '', source_url: d.source_url || '', verification_status: d.verification_status || '' }));
    $('kDates').textContent = 'Holidays: ' + this.dates.map((d) => `${d.date} ${displayLabel(d.label)}`).join(' · ');
    $('kVars').replaceChildren(h('label', {}, h('input', { type: 'checkbox', checked: true, onchange: (e) => $('kVars').querySelectorAll('input[data-v]').forEach((c) => { c.checked = e.target.checked; }) }), h('b', {}, ' all variables')),
      ...B.vars.filter((v) => v.active).map((v) => h('label', {}, h('input', { type: 'checkbox', 'data-v': v.variable_name, checked: true }), ' ' + v.variable_name + (v.shared ? ` (shared ×${v.assignments.length})` : ''))));
    $('kPreview').replaceChildren(); $('kCommit').disabled = true;
    $('dlgBulk').showModal();
  },
  chosen() { return [...$('kVars').querySelectorAll('input[data-v]:checked')].map((c) => c.dataset.v); },
  async run(commit) {
    const vars = this.chosen();
    if (!vars.length) { alert('Choose at least one variable.'); return; }
    try {
      const r = await api('POST', '/api/bulk-apply', { year: B.year, dates: this.dates, variables: vars, commit });
      $('kPreview').replaceChildren(h('table', { class: 'small' },
        h('tr', {}, h('th', {}, 'variable'), h('th', {}, commit ? 'added' : 'will add'), h('th', {}, 'already selected'), h('th', {}, 'skipped (manual exclusion)')),
        ...r.items.map((i) => h('tr', {}, h('td', {}, i.variable_name), h('td', {}, i.no_calendar ? 'no calendar for this year' : i.will_add.join(', ')),
          h('td', {}, i.already_selected.join(', ')), h('td', {}, i.skipped_manual_exclusion.join(', '))))),
        commit ? h('div', { class: 'msg ok' }, 'Saved to calendars.') : '');
      $('kCommit').disabled = commit;
      if (commit) B.summary();
    } catch (e) { $('kPreview').replaceChildren(h('div', { class: 'msg error' }, e.message)); }
  },
};
$('kPrev').onclick = () => Bulk.run(false);
$('kCommit').onclick = () => { if (confirm('Add these holidays to the chosen variable calendars now?')) Bulk.run(true); };
$('kVars').addEventListener('change', () => { $('kCommit').disabled = true; });

// ======================================================================
// Settings
// ======================================================================
$('btnSettings').onclick = async () => {
  try {
    const s = await api('GET', '/api/settings');
    $('sBreak').value = s.break;
    const p = s.providers.split(',').map((x) => x.trim());
    $('sProvBot').checked = p.includes('bot'); $('sProvIcs').checked = p.includes('ics');
    $('sBotUrl').value = s.bot_url; $('sIcsUrl').value = s.ics_url;
    $('dlgSettings').showModal();
  } catch (e) { alert('Cannot load settings: ' + e.message); }
};
$('sSave').onclick = async (e) => {
  e.preventDefault();
  const br = $('sBreak').value;
  if (br !== 'never' && !confirm(`Change break to "${br}"? This changes FreeSWITCH evaluation semantics and must be tested against the dialplan.`)) return;
  try {
    await api('PUT', '/api/settings', {
      break: br, providers: [$('sProvBot').checked && 'bot', $('sProvIcs').checked && 'ics'].filter(Boolean).join(','),
      bot_url: $('sBotUrl').value.trim(), ics_url: $('sIcsUrl').value.trim(),
    });
    $('dlgSettings').close();
    if (currentTab === 'b') B.summary();
  } catch (err) { alert('Save failed: ' + err.message); }
};

A.load();
