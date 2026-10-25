// One timeout policy: the overview runs live checks and may wait behind a compose operation;
// the rest are cheap reads. Aborts become a sentence a person can act on, never a DOM exception.
async function fetchT(url, opts, ms) {
  const ctrl = new AbortController();
  const limit = ms || 12000;
  const timer = setTimeout(() => ctrl.abort(), limit);
  try {
    return await fetch(url, Object.assign({}, opts, {signal: ctrl.signal}));
  } catch (e) {
    if (e && e.name === 'AbortError') {
      throw new Error(limit >= 1000 ? 'no answer within ' + Math.round(limit / 1000) + 's. The stack is busy; press Refresh in a moment' : 'no answer in time. The stack is busy; press Refresh in a moment');
    }
    throw e;
  } finally {
    clearTimeout(timer);
  }
}

/* The installation wizard. Every fact on the screen comes from /api/state or /api/events, which
   report the same steps the CLI runs. The browser keeps only the chosen scope and the project
   directory; nothing about the installation is invented here. */
(() => {
  const token = new URLSearchParams(location.search).get('t') || '';
  const $ = s => document.querySelector(s);
  const SVGNS = 'http://www.w3.org/2000/svg';
  const LABEL = {wait: 'Waiting', run: 'Running', ok: 'Done', fail: 'Failed', planned: 'Later'};
  const ETA = {solo: 'About 10 minutes', pilot: 'About 40 minutes'};

  let path = 'solo';
  let project = '';
  let state = null;
  let current = null;
  let autoAll = false;
  let notice = '';
  let runStarted = 0;
  let revealed = null; // credentials shown once, never stored
  let lastPaneID = null; // motion runs on a step change, not on every state tick
  let timer = null;
  const optInChecked = {}; // the person's own choices, kept only in this page

  try {
    path = localStorage.getItem('cardo.path') || 'solo';
    project = localStorage.getItem('cardo.project') || '';
  } catch (e) {}

  const clock = $('#clock');
  const tickClock = () => {
    const d = new Date();
    clock.textContent = String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0');
  };
  tickClock(); setInterval(tickClock, 10000);

  function el(tag, cls, text) {
    const n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text !== undefined) n.textContent = text;
    return n;
  }

  function icon(name, cls) {
    const svg = document.createElementNS(SVGNS, 'svg');
    svg.setAttribute('class', 'icon' + (cls ? ' ' + cls : ''));
    svg.setAttribute('aria-hidden', 'true');
    const use = document.createElementNS(SVGNS, 'use');
    use.setAttribute('href', '#i-' + name);
    svg.appendChild(use);
    return svg;
  }

  // ---- model ---------------------------------------------------------------
  function visibleSteps() {
    const out = [];
    for (const group of state.groups) {
      for (const step of group.steps) if (path === 'pilot' || !step.Pilot) out.push(step);
    }
    return out;
  }
  // The install is the steps that can run here. The proof is the engineer's own session, and it is
  // what turns "installed" into "verified". An opt-in step is the person's choice: it is shown,
  // it never runs unless they ask, and it never holds the install open.
  const countedSteps = () => visibleSteps().filter(s => !s.Planned && !s.OptIn);
  const requiredSteps = () => countedSteps().filter(s => !s.Optional && !s.Verify);
  const optInSteps = () => visibleSteps().filter(s => s.OptIn);
  const verifySteps = () => countedSteps().filter(s => s.Verify);
  const isVerified = () => isComplete() && verifySteps().every(s => s.state === 'ok');
  const implementedIndex = id => countedSteps().findIndex(s => s.ID === id);
  const plannedSteps = () => visibleSteps().filter(s => s.Planned);
  const stepIndex = id => requiredSteps().findIndex(s => s.ID === id);
  const nextPending = () => requiredSteps().find(s => s.state !== 'ok') || null;
  const firstPendingIndex = () => {
    const p = nextPending();
    return p ? stepIndex(p.ID) : requiredSteps().length;
  };
  const isComplete = () => requiredSteps().length > 0 && !nextPending();
  const runningStep = () => visibleSteps().find(s => s.state === 'run') || null;

  function setStep(id, patch) {
    for (const group of state.groups) {
      for (const step of group.steps) if (step.ID === id) Object.assign(step, patch);
    }
    if (current && current.ID === id) Object.assign(current, patch);
  }

  // ---- status --------------------------------------------------------------
  // One source of truth. A transient notice wins until the next event clears it.
  function statusText() {
    if (notice) return notice;
    const running = runningStep();
    if (running) {
      const secs = runStarted ? Math.max(0, Math.round((Date.now() - runStarted) / 1000)) : 0;
      return 'Running: ' + running.Name + (secs ? ' · ' + secs + 's' : '');
    }
    const failed = requiredSteps().find(s => s.state === 'fail');
    if (failed) return 'Failed: ' + failed.Name;
    if (isComplete()) return 'Complete';
    const p = nextPending();
    return p ? 'Ready: ' + p.Name : 'Ready';
  }
  function renderStatus() { $('#status').textContent = statusText(); }
  function setNotice(text) { notice = text; renderStatus(); }

  function startTimer() {
    if (timer) return;
    timer = setInterval(renderStatus, 1000);
  }
  function stopTimer() {
    if (!timer) return;
    clearInterval(timer); timer = null;
  }

  // ---- start screen --------------------------------------------------------
  function renderStartSelection() {
    document.querySelectorAll('.dep').forEach(o => {
      const on = o.dataset.path === path;
      o.setAttribute('aria-pressed', on ? 'true' : 'false');
      o.querySelector('.go').textContent = on ? 'Selected' : 'Select';
    });
    $('#eta').textContent = ETA[path] || '';
    $('#projectInput').value = project;
  }
  document.querySelectorAll('.dep').forEach(b => b.addEventListener('click', () => {
    path = b.dataset.path;
    try { localStorage.setItem('cardo.path', path); } catch (e) {}
    renderStartSelection();
    if (!$('#screenWiz').hidden) { current = nextPending() || countedSteps()[0] || null; render(); }
  }));
  $('#projectInput').addEventListener('input', e => {
    project = e.target.value.trim();
    try { localStorage.setItem('cardo.project', project); } catch (err) {}
  });

  $('#installAll').addEventListener('click', () => { autoAll = true; showWizard(); start('all'); });
  $('#stepThrough').addEventListener('click', () => { autoAll = false; showWizard(); });
  $('#runAll').addEventListener('click', () => { autoAll = true; start('all'); });

  $('#changeScope').addEventListener('click', () => {
    $('#screenWiz').hidden = true;
    $('#screenStart').hidden = false;
    try { localStorage.setItem('cardo.wizard', '0'); } catch (e) {}
    renderStartSelection();
  });
  $('#refresh').addEventListener('click', () => resync('State reloaded'));
  $('#railToggle').addEventListener('click', () => {
    const wiz = $('#wiz');
    const open = wiz.getAttribute('data-rail') !== 'open';
    wiz.setAttribute('data-rail', open ? 'open' : 'closed');
    $('#railToggle').setAttribute('aria-expanded', open ? 'true' : 'false');
  });

  function showWizard(keepCurrent) {
    if (!state) { setNotice('Loading installation state…'); setTimeout(() => showWizard(keepCurrent), 150); return; }
    $('#screenStart').hidden = true;
    $('#screenWiz').hidden = false;
    try { localStorage.setItem('cardo.wizard', '1'); } catch (e) {}
    if (!keepCurrent || !current) {
      current = nextPending() || (isComplete() ? null : requiredSteps()[0] || null);
    }
    render();
  }

  // ---- rendering -----------------------------------------------------------
  function render() {
    const counted = requiredSteps();
    const done = counted.filter(s => s.state === 'ok').length;
    notice = '';

    const seg = $('#segbar');
    seg.textContent = '';
    for (const step of counted) {
      const i = el('i');
      if (step.state === 'ok') i.className = 'ok';
      else if (step.state === 'run') i.className = 'run';
      else if (step.state === 'fail') i.className = 'fail';
      seg.appendChild(i);
    }

    const later = plannedSteps().length;
    const diagnostic = countedSteps().filter(s => s.Optional).length;
    // The rail header stays one line; the row tags already say Diagnostic, Proof and Later.
    $('#railCount').textContent = done + ' / ' + counted.length;
    $('#railCount').title = diagnostic + ' diagnostic · ' + verifySteps().length + ' proof · ' + later + ' later';
    const at = current ? stepIndex(current.ID) : -1;
    $('#pos').textContent = isComplete() && current === null
      ? 'Installation complete'
      : current && current.Verify
        ? 'Verification'
        : current && current.Optional
          ? 'Diagnostic'
          : 'Step ' + (at >= 0 ? at + 1 : Math.min(done + 1, counted.length)) + ' of ' + counted.length;
    $('#eta').hidden = isComplete() && current === null;

    // A restored or raced view must never leave the pane empty while work remains.
    if (!current && !isComplete()) current = nextPending() || requiredSteps()[0] || null;
    renderRail();
    renderPane();
    renderStatus();
    $('#runAll').disabled = !!runningStep();
    $('#railFoot').hidden = isComplete();
    updateEntry();
  }

  // The entry screen stays usable while a run is in flight: the buttons must not start a second one.
  function updateEntry() {
    const busy = !!runningStep();
    const install = $('#installAll');
    if (install) {
      install.disabled = busy;
      install.title = busy ? 'A run is already in progress' : '';
    }
    // Step through never starts work: it stays available so a person can watch a run they started.
    const step = $('#stepThrough');
    if (step) step.disabled = false;
  }

  function renderRail() {
    const list = $('#stepList');
    list.textContent = '';
    const limit = firstPendingIndex();
    let n = 0;
    visibleSteps().forEach(step => {
      const counted = !step.Planned && !step.Optional && !step.Verify && !step.OptIn;
      if (counted) n++;
      const li = el('li');
      const b = el('button', 'wiz-step');
      // An optional step that has not passed yet is waiting for the engineer, not failing.
      b.dataset.state = ((step.Optional || step.Verify || step.OptIn) && (step.state === 'fail' || step.state === 'wait')) ? 'wait' : (step.state || 'wait');
      b.appendChild(el('span', 'n', counted ? String(n).padStart(2, '0') : '··'));
      b.appendChild(el('span', 't', step.Name));

      const mark = el('span', 's');
      if (step.state === 'ok') mark.appendChild(icon('check', 'sm'));
      if (step.state === 'fail' && !step.Optional && !step.Verify && !step.OptIn) mark.appendChild(icon('alert', 'sm'));
      if (step.Optional) b.appendChild(el('span', 'later', 'Diagnostic'));
      if (step.Verify) b.appendChild(el('span', 'later', 'Proof'));
      if (step.OptIn) b.appendChild(el('span', 'later', 'Your choice'));
      if (step.Planned) b.appendChild(el('span', 'later', 'Later'));
      b.appendChild(mark);

      if (step.Planned) {
        b.disabled = true;
        b.title = 'Specified but not implemented yet';
      } else if (step.Verify && !isComplete()) {
        b.disabled = true;
        b.title = 'Available once the installation is complete';
      } else if (step.Optional && !isComplete()) {
        b.disabled = true;
        b.title = 'Appears after the first real session';
      } else if (step.OptIn) {
        // Always reachable: it is the person's choice, and it never waits for another step.
        b.addEventListener('click', () => {
          current = step;
          autoAll = false;
          const wiz = $('#wiz');
          if (wiz.getAttribute('data-rail') === 'open') wiz.setAttribute('data-rail', 'closed');
          render();
        });
      } else if (!step.Optional && !step.Verify && stepIndex(step.ID) > limit) {
        b.disabled = true;
        b.title = 'Complete the previous steps first';
      } else {
        b.addEventListener('click', () => {
          current = step;
          autoAll = false;
          const wiz = $('#wiz');
          if (wiz.getAttribute('data-rail') === 'open') wiz.setAttribute('data-rail', 'closed');
          render();
        });
      }
      if (current && step.ID === current.ID) b.setAttribute('aria-current', 'true');
      li.appendChild(b);
      list.appendChild(li);
    });
  }

  function renderPane() {
    const pane = $('#pane');
    pane.textContent = '';
    const entering = current && current.ID !== lastPaneID;
    lastPaneID = current ? current.ID : null;
    if (entering) {
      pane.classList.remove('enter');
      void pane.offsetWidth; // restart the animation for a new step
      pane.classList.add('enter');
    }
    if (isComplete() && current === null) { renderComplete(pane); return; }
    if (!current) return;

    const counted = requiredSteps();
    const idx = stepIndex(current.ID);

    const head = el('div', 'wiz-head');
    const meta = el('div', 'wiz-meta');
    meta.appendChild(el('span', '', current.Verify ? 'Verification' : (current.Optional ? 'Diagnostic' : 'Step ' + (idx + 1) + ' of ' + counted.length)));
    meta.appendChild(el('span', '', '·'));
    meta.appendChild(el('b', '', current.Group));
    head.appendChild(meta);
    head.appendChild(el('h1', 'wiz-title', current.Name));
    if (current.Title) head.appendChild(el('p', 'wiz-sub', current.Title));
    const body = el('div', 'wiz-body');
    body.appendChild(resultLine(current));
    for (const p of current.Body || []) body.appendChild(el('p', '', p));
    head.appendChild(body);
    pane.appendChild(head);

    if (current.state === 'fail' && !current.Optional && !current.Verify) {
      const box = el('div', 'wiz-error');
      const lbl = el('span', 'lbl');
      lbl.appendChild(icon('alert', 'sm'));
      lbl.appendChild(document.createTextNode(' What failed'));
      box.appendChild(lbl);
      box.appendChild(el('p', 'cause', current.cause || 'The step did not report a cause.'));
      const rec = el('div', 'rec');
      rec.appendChild(el('b', '', 'Recovery'));
      rec.appendChild(el('span', '', current.fix || 'No recovery is recorded for this failure.'));
      box.appendChild(rec);
      pane.appendChild(box);
    }

    if (current.KV && current.KV.length) {
      const sec = el('div', 'wiz-section');
      sec.appendChild(el('h3', '', 'Details'));
      const table = el('table', 'wiz-facts');
      for (const pair of current.KV) {
        const tr = el('tr');
        tr.appendChild(el('td', '', pair[0]));
        tr.appendChild(el('td', '', pair[1]));
        table.appendChild(tr);
      }
      sec.appendChild(table);
      pane.appendChild(sec);
    }

    let input = null;
    let optInBox = null;
    if (current.OptIn) {
      // The checkbox is unticked. Nothing runs until the person ticks it: this step reads files
      // outside the stack, and "install everything" never takes it.
      const sec = el('div', 'wiz-section');
      sec.appendChild(el('h3', '', 'Your choice'));
      const label = el('label', 'checkline');
      optInBox = el('input');
      optInBox.type = 'checkbox';
      optInBox.checked = optInChecked[current.ID] === true;
      optInBox.setAttribute('aria-label', 'Import my Claude Code history');
      optInBox.addEventListener('change', () => {
        optInChecked[current.ID] = optInBox.checked;
        render();
      });
      label.appendChild(optInBox);
      label.appendChild(document.createTextNode(' Import my Claude Code history'));
      sec.appendChild(label);
      sec.appendChild(el('p', '', 'A one-time import of the sessions already on this machine. Counts, tokens and timing only; the prompt text is measured and discarded. Nothing runs in the background afterwards.'));
      pane.appendChild(sec);
    }
    if (current.Input === 'project') {
      const sec = el('div', 'wiz-section');
      sec.appendChild(el('h3', '', 'Project directory'));
      input = el('input');
      input.type = 'text';
      input.className = 'input';
      input.value = project;
      input.placeholder = '/path/to/project';
      input.setAttribute('aria-label', 'Project directory');
      input.addEventListener('input', () => {
        project = input.value.trim();
        try { localStorage.setItem('cardo.project', project); } catch (e) {}
      });
      sec.appendChild(input);
      sec.appendChild(el('p', '', 'The settings file is written to this directory as .claude/settings.json.'));
      pane.appendChild(sec);
    }

    if (current.ID === 'secrets' && revealed && revealed.id === 'secrets') {
      const sec = el('div', 'wiz-section');
      sec.appendChild(el('h3', '', 'Credentials, shown once'));
      const table = el('table', 'wiz-facts');
      for (const [name, value] of Object.entries(revealed.values)) {
        const tr = el('tr');
        tr.appendChild(el('td', '', name));
        tr.appendChild(el('td', '', value));
        const td = el('td');
        const copy = el('button', 'copy');
        copy.appendChild(icon('copy', 'sm'));
        copy.appendChild(document.createTextNode(' Copy'));
        copy.addEventListener('click', () => copyText(value, copy));
        td.appendChild(copy);
        tr.appendChild(td);
        table.appendChild(tr);
      }
      sec.appendChild(table);
      sec.appendChild(el('p', '', 'Also in deploy/compose/.env with mode 0600. This panel is not stored anywhere and disappears when the page is reloaded.'));
      pane.appendChild(sec);
    }

    if (current.Cmd) {
      const sec = el('div', 'wiz-section');
      sec.appendChild(el('h3', '', 'Command'));
      const cmd = el('div', 'cmd');
      const text = commandText(current);
      cmd.appendChild(el('code', '', text));
      const copy = el('button', 'copy');
      copy.appendChild(icon('copy', 'sm'));
      copy.appendChild(document.createTextNode(' Copy'));
      if (text.includes('<directory>')) {
        copy.disabled = true;
        copy.title = 'Enter the project directory first';
      } else {
        copy.addEventListener('click', () => copyText(text, copy));
      }
      cmd.appendChild(copy);
      sec.appendChild(cmd);
      pane.appendChild(sec);
    }

    if (current.state === 'fail' && !current.Optional && !current.Verify) {
      const sec = el('div', 'wiz-section');
      sec.appendChild(el('h3', '', 'Blocked'));
      sec.appendChild(el('p', '', 'The installation cannot continue until this step succeeds. The next step stays locked.'));
      pane.appendChild(sec);
    } else if ((current.Optional || current.Verify) && current.fix) {
      const sec = el('div', 'wiz-section');
      sec.appendChild(el('h3', '', 'When it appears'));
      sec.appendChild(el('p', '', current.fix));
      pane.appendChild(sec);
    } else {
      const nextStep = countedSteps()[implementedIndex(current.ID) + 1];
      if (nextStep) {
        const sec = el('div', 'wiz-section');
        sec.appendChild(el('h3', '', nextStep.Verify ? 'Proof next' : (nextStep.Optional ? 'Diagnostic next' : 'Next step')));
        const line = el('p');
        line.style.margin = '0';
        const label = nextStep.Optional
          ? nextStep.Name
          : (stepIndex(nextStep.ID) + 1) + '. ' + nextStep.Name;
        line.appendChild(el('b', '', label));
        line.appendChild(document.createTextNode(' — ' + firstSentence((nextStep.Body && nextStep.Body[0]) || '')));
        sec.appendChild(line);
        pane.appendChild(sec);
      }
    }

    const actions = el('div', 'wiz-actions');
    const busy = current.state === 'run';
    const readyLabel = current.Action === 'Check' ? 'Check and continue'
      : current.Action === 'Start' ? 'Start and continue'
      : current.Action === 'Generate and write' ? 'Generate secrets and continue'
      : current.OptIn ? 'Import and continue'
      : 'Install and continue';
    const retryLabel = current.Verify ? 'Check again' : 'Retry';
    const primary = el('button', 'act primary',
      busy ? 'Running…' : current.state === 'ok' ? 'Continue' : current.state === 'fail' ? retryLabel : readyLabel);
    primary.disabled = busy;
    primary.addEventListener('click', () => {
      if (current.state === 'ok') { advance(); return; }
      if (current.OptIn && !optInChecked[current.ID]) {
        setNotice('Tick the box to import your history, or leave it and continue');
        return;
      }
      if (current.Input === 'project' && !project) {
        setNotice('Enter the project directory, then continue');
        if (input) input.focus();
        return;
      }
      autoAll = false;
      runStep(current.ID);
    });
    actions.appendChild(primary);

    if (current.state === 'ok') {
      const again = el('button', 'act ghost', 'Run again');
      again.disabled = busy;
      again.addEventListener('click', () => { autoAll = false; runStep(current.ID); });
      actions.appendChild(again);
    }
    if (isComplete()) {
      const back = el('button', 'act ghost', 'Back to summary');
      back.addEventListener('click', () => { current = null; render(); });
      actions.appendChild(back);
    }
    pane.appendChild(actions);
  }

  function resultLine(step) {
    const line = el('div', 'wiz-result');
    const shown = (step.Optional || step.Verify) && step.state === 'fail' ? 'wait' : (step.state || 'wait');
    const lamp = el('span', 'lamp');
    lamp.dataset.s = shown;
    lamp.appendChild(el('b'));
    lamp.appendChild(el('span', '', step.state === 'planned' ? 'Later' : (LABEL[shown] || 'Waiting')));
    line.appendChild(lamp);
    const value = step.state === 'run' ? 'working…' : (step.value || (step.state === 'planned' ? 'not implemented' : 'no result yet'));
    line.appendChild(el('span', 'v', value));
    return line;
  }

  function commandText(step) {
    if (step.ID === 'connect') {
      return project ? 'cardo connect --project ' + project : step.Cmd;
    }
    return step.Cmd;
  }

  function renderComplete(pane) {
    const counted = requiredSteps();
    const later = plannedSteps();
    const proof = verifySteps()[0];
    const diagnostic = countedSteps().find(s => s.Optional);
    const wrap = el('div', 'pane-done');
    wrap.appendChild(el('h2', '', isVerified() ? 'Verified' : 'Installation complete'));
    if (proof) {
      wrap.appendChild(el('p', '', isVerified()
        ? 'Verified with your own session: ' + proof.value + '.'
        : 'Installed. Verification waits for your first session: run Claude Code once in an instrumented project, then check again.'));
    }
    if (diagnostic && diagnostic.value) {
      wrap.appendChild(el('p', '', 'Diagnostic probe: ' + diagnostic.value + '.'));
    }
    wrap.appendChild(el('p', '', 'The stack is installed and the settings file is in place.'
      + (later.length ? ' ' + later.length + ' further steps are specified and not implemented: ' + later.map(s => s.Name.toLowerCase()).join(', ') + '.' : '')));

    const facts = el('div', 'facts');
    const f1 = el('div'); f1.appendChild(el('b', '', counted.filter(s => s.state === 'ok').length + ' / ' + counted.length)); f1.appendChild(el('span', '', 'Required steps complete')); facts.appendChild(f1);
    const f2 = el('div'); f2.appendChild(el('b', '', '3001')); f2.appendChild(el('span', '', 'Grafana on loopback')); facts.appendChild(f2);
    const f3 = el('div'); f3.appendChild(el('b', '', '0')); f3.appendChild(el('span', '', 'Bytes outbound')); facts.appendChild(f3);
    wrap.appendChild(facts);

    wrap.appendChild(el('p', '', 'Grafana signs in as admin. The generated password is in deploy/compose/.env (mode 0600), key GRAFANA_PASSWORD.'));

    const actions = el('div', 'actions');
    const open = el('a', 'act primary');
    open.appendChild(document.createTextNode('Open dashboards'));
    open.appendChild(icon('external', 'sm'));
    open.href = 'http://127.0.0.1:3001';
    open.target = '_blank';
    open.rel = 'noreferrer';
    actions.appendChild(open);
    if (proof && !isVerified()) {
      const verify = el('button', 'act ghost', 'Check my session');
      verify.addEventListener('click', () => { autoAll = false; current = proof; render(); });
      actions.appendChild(verify);
    }
    const check = el('button', 'act ghost', 'Run checks');
    check.addEventListener('click', () => { autoAll = false; start('check'); });
    actions.appendChild(check);
    wrap.appendChild(actions);
    wrap.appendChild(el('p', '', 'Checks without the browser: cardo check. Updates and repairs: cardo up.'));
    pane.appendChild(wrap);
  }

  function firstSentence(text) {
    const cut = text.indexOf('. ');
    return cut > 40 ? text.slice(0, cut + 1) : text;
  }

  // ---- actions -------------------------------------------------------------
  function advance() {
    const next = nextPending();
    current = next || null;
    render();
  }

  async function post(body) {
    return fetchT('/api/run', {
      method: 'POST',
      headers: {'Content-Type': 'application/json', 'X-Cardo-Token': token},
      body: JSON.stringify(body)
    });
  }

  async function runStep(id) {
    setStep(id, {state: 'run', cause: '', fix: ''});
    runStarted = Date.now();
    startTimer();
    render();
    let res;
    try {
      res = await post(id === 'connect' && project ? {step: id, project} : {step: id});
    } catch (e) {
      stopTimer();
      setStep(id, {state: 'fail', cause: 'The installer could not be reached: ' + e.message, fix: 'Check that the terminal running cardo ui is still open, then retry.'});
      render();
      return;
    }
    if (res.status === 401) { showExpired(); return; }
    if (!res.ok) {
      stopTimer();
      setStep(id, {state: 'fail', cause: (await res.text()).trim(), fix: 'Restart the installer with cardo ui and retry.'});
      render();
    }
  }

  async function start(mode) {
    if (mode === 'all') { current = requiredSteps()[0] || current; render(); }
    setNotice(mode === 'check' ? 'Running the checks…' : 'Starting the installation…');
    runStarted = Date.now();
    startTimer();
    let res;
    try {
      res = await post(mode === 'check' ? {step: 'check'} : {step: 'all', project: project || undefined});
    } catch (e) {
      stopTimer();
      setNotice('The installer could not be reached — is cardo ui still running?');
      return;
    }
    if (res.status === 401) { showExpired(); return; }
    if (!res.ok) {
      stopTimer();
      const text = (await res.text()).trim();
      setNotice(/already running/i.test(text) ? 'A run is already in progress' : 'Cannot start: ' + text);
    }
  }

  function copyText(text, button) {
    const ok = () => {
      const old = button.textContent;
      button.textContent = 'Copied';
      setTimeout(() => { button.textContent = old; }, 1400);
    };
    const fail = () => {
      const old = button.textContent;
      button.textContent = 'Copy failed — select the text';
      setTimeout(() => { button.textContent = old; }, 2400);
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(ok).catch(fail);
    } else { fail(); }
  }

  // ---- events and resync ---------------------------------------------------
  function update(ev) {
    if (ev.note === 'done') {
      stopTimer();
      const wasAuto = autoAll;
      autoAll = false; // the batch is over; later clicks are the person's, not the run's
      if (wasAuto) {
        // Only a batch run moves the view by itself; a single step or a step the person opened stays.
        const failed = requiredSteps().find(s => s.state === 'fail');
        if (failed) current = failed;
        else if (!nextPending()) current = null;
      }
      render();
      return;
    }
    if (ev.reveal) revealed = {id: ev.id, values: ev.reveal};
    const patch = {state: ev.state};
    if (ev.value) patch.value = ev.value;
    if (ev.cause !== undefined) patch.cause = ev.cause;
    if (ev.fix !== undefined) patch.fix = ev.fix;
    setStep(ev.id, patch);
    if (ev.state === 'run') { runStarted = Date.now(); startTimer(); }
    if (ev.state === 'ok' || ev.state === 'fail') {
      stopTimer();
      const step = visibleSteps().find(s => s.ID === ev.id);
      if (step && (!current || current.state === 'run' || autoAll)) current = step;
    }
    render();
    if (ev.state === 'ok' && autoAll && current && current.ID === ev.id) {
      setTimeout(() => {
        if (current && current.ID === ev.id && current.state === 'ok') advance();
      }, 1200);
    }
  }

  async function load() {
    const res = await fetchT('/api/state');
    if (res.status === 401) { showExpired(); return false; }
    if (!res.ok) { setNotice('The installer is not responding'); return false; }
    state = await res.json();
    if (current) {
      const fresh = visibleSteps().find(s => s.ID === current.ID);
      current = fresh || nextPending() || null;
    }
    return true;
  }

  async function resync(message) {
    const before = current && current.ID;
    const ok = await load();
    if (!ok) return;
    if (before) current = visibleSteps().find(s => s.ID === before) || current;
    render();
    if (message) setNotice(message);
  }

  function showExpired() {
    document.body.textContent = '';
    const shell = el('div', 'shell');
    const pane = el('div', 'pane-done');
    pane.appendChild(el('h2', '', 'This link is no longer valid'));
    pane.appendChild(el('p', '', 'The installer generates a new token each time it starts, so an older link cannot perform actions. Restart the installer and open the URL it prints, including the token.'));
    const cmd = el('div', 'cmd');
    cmd.appendChild(el('code', '', 'cd /Users/sodx/Projects/cardo && ./bin/cardo ui'));
    pane.appendChild(cmd);
    pane.appendChild(el('p', '', 'The stack keeps running while the installer is restarted. Details: docs/design/quickstart-launcher.md.'));
    shell.appendChild(pane);
    document.body.appendChild(shell);
  }

  // ---- boot ----------------------------------------------------------------
  renderStartSelection();
  load().then(ok => {
    if (!ok) return;
    // A reload in the middle of an installation must return to the wizard, not to the entry screen.
    let resume = false;
    try { resume = localStorage.getItem('cardo.wizard') === '1'; } catch (e) {}
    if (resume) showWizard(true);
    const es = new EventSource('/api/events');
    es.onopen = () => resync('');
    es.onerror = () => setNotice('Connection to the installer lost — press Refresh');
    es.onmessage = m => {
      try { update(JSON.parse(m.data)); } catch (e) { /* a malformed event must not stop the stream */ }
    };
  });
  window.cardoBoard = {showWizard, isComplete, requiredSteps, getState: () => state};
})();

// ---------------------------------------------------------------- surfaces
// This block runs at top level, outside the wizard's IIFE, so it carries its own two helpers.
const $ = (sel) => document.querySelector(sel);
const el = (tag, cls, text) => { const n = document.createElement(tag); if (cls) n.className = cls; if (text != null) n.textContent = text; return n; };
// The board is a console: Overview is the landing, Install is the old wizard, Settings owns the
// knobs. One router, no duplicated rendering of install state.

const SURFACE_KEY = 'cardo.surface';
let surface = 'overview';

function setWhere() {
  const w = document.querySelector('.where');
  if (!w) return;
  const label = surface === 'install' ? 'Installation' : surface === 'catalog' ? 'Catalog' : surface === 'teams' ? 'Teams' : surface === 'settings' ? 'Settings' : surface === 'coach' ? 'Coach' : 'Overview';
  w.textContent = label + ' · local only · 127.0.0.1';
}

function showSurface(name, opts) {
  const fromHash = opts && opts.fromHash;
  surface = ['overview', 'install', 'catalog', 'teams', 'coach', 'settings'].includes(name) ? name : 'overview';
  localStorage.setItem(SURFACE_KEY, surface);
  const active = 'surface' + surface[0].toUpperCase() + surface.slice(1);
  for (const section of document.querySelectorAll('.surface')) {
    section.hidden = section.id !== active;
  }
  for (const b of document.querySelectorAll('.surf')) {
    if (b.dataset.surface === surface) b.setAttribute('aria-current', 'page');
    else b.removeAttribute('aria-current');
  }
  setWhere();
  document.title = (surface === 'install' ? 'Installation' : surface === 'catalog' ? 'Catalog' : surface === 'teams' ? 'Teams' : surface === 'settings' ? 'Settings' : surface === 'coach' ? 'Coach' : 'Overview') + ' — Cardo';
  if (!fromHash) history.replaceState(null, '', '#/' + surface);
  if (surface === 'overview') loadOverview();
  if (surface === 'catalog') loadCatalog();
  if (surface === 'teams') loadTeams();
  if (surface === 'settings') loadSettings();
}

function factCell(label, value, tone) {
  const d = el('div');
  const b = el('b', '', value);
  if (tone) b.className = tone;
  d.appendChild(b);
  d.appendChild(el('span', '', label));
  return d;
}

let ovSeq = 0;
async function loadOverview(fresh) {
  const seq = ++ovSeq;
  const facts = $('#ovFacts');
  const next = $('#ovNext');
  if (!facts) return;
  let o;
  try {
    const r = await fetchT('/api/overview' + (fresh ? '?fresh=1' : ''), null, 25000);
    if (!r.ok) throw new Error('HTTP ' + r.status);
    o = await r.json();
    if (seq !== ovSeq) return;
  } catch (e) {
    if (seq !== ovSeq) return;
    facts.innerHTML = '';
    next.innerHTML = '';
    next.dataset.tone = 'warn';
    next.appendChild(el('div', 'state-error', 'Cannot read the overview: ' + e.message + '.'));
    const again = el('button', 'act ghost', 'Try again');
    again.addEventListener('click', loadOverview);
    next.appendChild(again);
    return;
  }
  const asOf = $('#ovAsOf');
  if (asOf) asOf.textContent = 'as of ' + (o.asOf || '—') + ' · read from the stack, not remembered';
  const warnWrap = $('#ovWarningsWrap');
  const warnList = $('#ovWarnings');
  warnList.innerHTML = '';
  for (const w of o.warnings || []) warnList.appendChild(el('li', '', w));
  warnWrap.hidden = (o.warnings || []).length === 0;
  const chWrap = $('#ovChangesWrap');
  const chBody = $('#ovChanges');
  chBody.innerHTML = '';
  for (const c of o.changes || []) {
    const tr = el('tr');
    tr.appendChild(el('td', '', (c.at || '').slice(5, 16)));
    tr.appendChild(el('td', '', c.action));
    tr.appendChild(el('td', '', c.object));
    tr.appendChild(el('td', '', c.note || ''));
    chBody.appendChild(tr);
  }
  chWrap.hidden = (o.changes || []).length === 0;

  facts.innerHTML = '';
  facts.appendChild(factCell('Stack', o.stack.ok ? o.stack.value : 'failing', o.stack.ok && !/waiting/i.test(o.stack.value) ? 'ok' : ''));
  facts.appendChild(factCell('Sessions · 7d', o.sessions.value || '—', o.sessions.value && !/^no /i.test(o.sessions.value) ? 'ok' : 'mute'));
  facts.appendChild(factCell('Admin API', o.poller.configured ? (o.poller.lastDay ? 'last ' + o.poller.lastDay : 'key set') : 'off',
    o.poller.configured ? 'ok' : 'mute'));
  facts.appendChild(factCell('Warnings', String((o.warnings || []).length), (o.warnings || []).length ? 'bad' : 'mute'));

  next.innerHTML = '';
  next.dataset.tone = o.next.surface === 'overview' ? '' : 'warn';
  const left = el('div');
  left.appendChild(el('h2', '', o.next.title));
  left.appendChild(el('p', '', o.next.detail));
  next.appendChild(left);
  if (o.next.surface !== 'overview') {
    const go = el('button', 'act ghost', o.next.surface === 'install' ? 'Open install' : 'Open settings');
    go.addEventListener('click', () => showSurface(o.next.surface));
    next.appendChild(go);
  }

  const wrap = $('#ovSessionWrap');
  const card = $('#ovSession');
  card.innerHTML = '';
  if (!o.session) { wrap.hidden = true; return; }
  wrap.hidden = false;
  const rows = [
    ['Session', o.session.id.slice(0, 12) + '…'],
    ['App version', o.session.appVersion || '—'],
    ['Started', o.session.startedAt],
    ['Last event', o.session.lastEventAt],
    ['Ended', o.session.ended ? (o.session.endReason || 'yes') : 'still open'],
    ['Prompts', String(o.session.prompts)],
    ['OTel · hooks', (o.session.hasOtel ? 'otel' : 'no otel') + ' · ' + (o.session.hasHooks ? 'hooks' : 'no hooks')],
    ['Artifacts', (o.session.artifacts || []).join(', ') || 'none loaded'],
  ];
  const dl = el('dl', 'ov-session');
  for (const [k, v] of rows) {
    dl.appendChild(el('dt', '', k));
    dl.appendChild(el('dd', '', v));
  }
  card.appendChild(dl);
}

async function loadSettings() {
  const body = $('#setBody');
  if (!body) return;
  body.innerHTML = '';
  for (let i = 0; i < 3; i++) {
    const g = el('div', 'wiz-section');
    g.appendChild(el('div', 'skeleton w40'));
    g.appendChild(el('div', 'skeleton w80'));
    g.appendChild(el('div', 'skeleton w60'));
    body.appendChild(g);
  }
  let s;
  try {
    const r = await fetchT('/api/settings', null, 10000);
    if (!r.ok) throw new Error('HTTP ' + r.status);
    s = await r.json();
  } catch (e) {
    body.innerHTML = '';
    body.innerHTML = '';
    body.appendChild(el('p', 'state-error', 'Cannot read settings: ' + e.message + '.'));
    const again = el('button', 'act ghost', 'Try again');
    again.addEventListener('click', loadSettings);
    body.appendChild(again);
    return;
  }
  body.innerHTML = '';

  try { localStorage.setItem('cardo.path', s.pilotMode ? 'pilot' : 'solo'); } catch (e) {}

  // Installation: state, account type, the way back into the step-level repair surface.
  const inst = el('div', 'set-group');
  inst.appendChild(el('h3', '', 'Installation'));
  inst.appendChild(el('p', '', 'The installer is also the repair surface: each step explains its cause and can be run alone. It is not a daily tab, so it lives here once the installation is complete.'));
  const stateLine = el('p', 'ro', '');
  let stateText = 'Unknown · open the installer to see the steps';
  try {
    const sr = await fetchT('/api/state', null, 8000);
    if (sr.ok) {
      const sd = await sr.json();
      const steps = (sd.groups || []).flatMap(g => g.steps).filter(x => !x.Planned && !x.Optional && !x.Verify);
      const ok = steps.filter(x => x.OK).length;
      if (steps.length && ok === steps.length) stateText = 'Complete · ' + ok + ' of ' + steps.length + ' required steps';
      else if (ok === 0) stateText = 'Not run since this board started · open the installer to run or repair';
      else stateText = 'Needs attention · ' + ok + ' of ' + steps.length + ' required steps';
    }
  } catch (e) { stateText = 'Cannot read the step state: ' + e.message; }
  stateLine.textContent = stateText;
  inst.appendChild(stateLine);

  const typeRow = el('div', 'set-row');
  typeRow.appendChild(el('label', '', 'Account type'));
  const typeWrap = el('div', 'seg-choice');
  const mkType = (label, pilot) => {
    const b = el('button', 'act ghost', label);
    b.setAttribute('aria-pressed', (s.pilotMode ? 'pilot' : 'solo') === (pilot ? 'pilot' : 'solo') ? 'true' : 'false');
    b.addEventListener('click', async () => {
      const r = await fetchT('/api/settings', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({pilotMode: pilot})});
      if (!r.ok) { stateLine.textContent = 'Not changed: ' + (await r.text()).trim(); return; }
      try { localStorage.setItem('cardo.path', pilot ? 'pilot' : 'solo'); } catch (e) {}
      await loadSettings();
      const board = window.cardoBoard;
      if (board && board.getState() && !document.querySelector('#screenWiz').hidden) board.showWizard(true);
    });
    return b;
  };
  typeWrap.appendChild(mkType('Single engineer', false));
  typeWrap.appendChild(mkType('Team pilot', true));
  typeRow.appendChild(typeWrap);
  inst.appendChild(typeRow);
  inst.appendChild(el('p', 'hint', 'Changing the type changes which steps the installer shows. It does not reinstall or delete anything; run a repair from the installer if a step needs another pass.'));

  const projRow = el('div', 'set-row');
  projRow.appendChild(el('label', '', 'Project directory'));
  const projInput = el('input', 'input');
  try { projInput.value = localStorage.getItem('cardo.project') || ''; } catch (e) {}
  projInput.placeholder = '/path/to/a/project';
  projInput.addEventListener('input', () => {
    try { localStorage.setItem('cardo.project', projInput.value.trim()); } catch (e) {}
  });
  projRow.appendChild(projInput);
  projRow.appendChild(el('span', 'ro', 'used by the connect step'));
  inst.appendChild(projRow);

  const instActions = el('div', 'set-row');
  instActions.appendChild(el('label', '', ''));
  const openInstall = el('button', 'act ghost', 'Open install and repair');
  openInstall.addEventListener('click', () => {
    showSurface('install');
    const board = window.cardoBoard;
    if (board && board.getState()) board.showWizard(true);
  });
  const runChecks = el('button', 'act ghost', 'Run checks');
  runChecks.addEventListener('click', () => { showSurface('overview'); loadOverview(true); });
  const actWrap = el('div', 'seg-choice');
  actWrap.appendChild(openInstall);
  actWrap.appendChild(runChecks);
  instActions.appendChild(actWrap);
  inst.appendChild(instActions);
  body.appendChild(inst);

  // Ingestion
  const ing = el('div', 'set-group');
  ing.appendChild(el('h3', '', 'Ingestion'));
  ing.appendChild(el('p', '', 'The Admin API path is optional. Saving a key writes it to deploy/compose/.env with mode 0600 and starts the poller inside the stack.'));
  const keyRow = el('div', 'set-row');
  keyRow.appendChild(el('label', '', 'Admin key'));
  const key = el('input', 'input');
  key.type = 'password';
  key.autocomplete = 'off';
  key.placeholder = s.poller.configured ? 'key saved — paste a new one to replace it' : 'sk-ant-admin…';
  keyRow.appendChild(key);
  keyRow.appendChild(el('span', 'ro', s.poller.configured ? 'configured' : 'not configured'));
  ing.appendChild(keyRow);
  const intRow = el('div', 'set-row');
  intRow.appendChild(el('label', '', 'Poll interval'));
  const inter = el('input', 'input');
  inter.value = s.poller.interval || '24h';
  intRow.appendChild(inter);
  const saveAdminBtn = el('button', 'act primary', 'Save key and start');
  intRow.appendChild(saveAdminBtn);
  ing.appendChild(intRow);
  const adminStatus = el('p', 'hint', '');
  adminStatus.style.gridColumn = '2 / -1';
  ing.appendChild(adminStatus);
  saveAdminBtn.addEventListener('click', async () => {
    adminStatus.textContent = 'Saving…';
    const r = await fetchT('/api/admin', {
      method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({key: key.value.trim(), interval: inter.value.trim()}),
    });
    if (!r.ok) { adminStatus.textContent = (await r.text()).trim(); return; }
    key.value = '';
    adminStatus.textContent = 'Saved; the poller is starting.';
    setTimeout(loadSettings, 1500);
  });
  body.appendChild(ing);

  // Privacy
  const priv = el('div', 'set-group');
  priv.appendChild(el('h3', '', 'Privacy'));
  priv.appendChild(el('p', '', 'The floor protects people. Any cohort below this size is suppressed rather than shown small.'));
  const mgRow = el('div', 'set-row');
  mgRow.appendChild(el('label', '', 'Minimum group size'));
  const mg = el('input', 'input');
  mg.type = 'number';
  mg.min = '5';
  mg.max = '100';
  mg.value = String(s.minGroupSize);
  mgRow.appendChild(mg);
  const mgBtn = el('button', 'act ghost', 'Apply');
  mgRow.appendChild(mgBtn);
  priv.appendChild(mgRow);
  const mgStatus = el('p', 'hint', 'Views read the highest version of this value; nothing is edited in place.');
  priv.appendChild(mgStatus);

  const retRow = el('div', 'set-row');
  retRow.appendChild(el('label', '', 'Retention (days)'));
  const ret = el('input', 'input');
  ret.type = 'number';
  ret.min = '0';
  ret.max = '3650';
  ret.value = String(s.retentionDays || 0);
  retRow.appendChild(ret);
  const retBtn = el('button', 'act ghost', 'Apply');
  retBtn.textContent = 'Apply';
  retRow.appendChild(retBtn);
  priv.appendChild(retRow);
  // The effective value, and why it is that one. A number with no history is a guess (ADR-0053).
  const retStatus = el('p', 'hint', s.retentionNote || '');
  priv.appendChild(retStatus);
  const retExtra = el('p', 'hint', '0 keeps everything. Anything else deletes raw rows older than that after every poll; silver and gold are views, so they shorten too.');
  priv.appendChild(retExtra);
  retBtn.addEventListener('click', async () => {
    retStatus.textContent = 'Applying…';
    try {
      const r = await fetchT('/api/settings', {
        method: 'POST', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({retentionDays: Number(ret.value || 0)})
      });
      if (!r.ok) throw new Error((await r.text()).trim());
      retStatus.textContent = 'Saved. The poller applies it after the next run.';
      loadSettings();
    } catch (e) {
      retStatus.textContent = 'Not changed: ' + e.message;
    }
  });
  mgBtn.addEventListener('click', async () => {
    mgStatus.textContent = 'Applying…';
    const r = await fetchT('/api/settings', {
      method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({minGroupSize: Number(mg.value)}),
    });
    mgStatus.textContent = r.ok ? 'Applied. New queries use ' + mg.value + '.' : (await r.text()).trim();
  });
  body.appendChild(priv);

  // Catalog
  const cat = el('div', 'set-group');
  cat.appendChild(el('h3', '', 'Catalog'));
  cat.appendChild(el('p', '', 'The artifacts this organization rolled out, as slugs the views label as reach rather than organic spread.'));
  const oaRow = el('div', 'set-row');
  oaRow.appendChild(el('label', '', 'Org artifacts'));
  const oa = el('input', 'input');
  oa.value = s.orgArtifacts || '';
  oa.placeholder = 'review-pr, deploy-check';
  oaRow.appendChild(oa);
  const oaBtn = el('button', 'act ghost', 'Apply');
  oaRow.appendChild(oaBtn);
  cat.appendChild(oaRow);
  const oaStatus = el('p', 'hint', 'Comma-separated slugs. Written to settings and to .env, so the collector and the views agree.');
  cat.appendChild(oaStatus);
  oaBtn.addEventListener('click', async () => {
    oaStatus.textContent = 'Applying…';
    const r = await fetchT('/api/settings', {
      method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({orgArtifacts: oa.value.trim()}),
    });
    oaStatus.textContent = r.ok ? 'Applied.' : (await r.text()).trim();
  });
  body.appendChild(cat);

  // Stack (read-only)
  const st = el('div', 'set-group');
  st.appendChild(el('h3', '', 'Stack'));
  st.appendChild(el('p', '', 'Read-only: these come from the compose file and the binary, not from this screen.'));
  for (const [label, value] of [
    ['Ports', (s.ports || []).join(', ')],
    ['Compose directory', s.dir],
    ['Version', s.version || 'dev'],
  ]) {
    const row = el('div', 'set-row');
    row.appendChild(el('label', '', label));
    row.appendChild(el('span', 'ro', value));
    st.appendChild(row);
  }
  body.appendChild(st);
}

document.querySelectorAll('.surf').forEach(b => b.addEventListener('click', () => showSurface(b.dataset.surface)));
$('#ovRefresh')?.addEventListener('click', () => loadOverview(true));
const boot = (location.hash || '').replace(/^#\//, '').split('/');
showSurface(boot[0] || localStorage.getItem(SURFACE_KEY) || 'overview');
if (boot[1]) setTimeout(() => openArtifact(decodeURIComponent(boot[1])), 300);
window.addEventListener('hashchange', () => {
  const p = (location.hash || '').replace(/^#\//, '').split('/');
  if (p[0]) showSurface(p[0], {fromHash: true});
  if (p[1]) openArtifact(decodeURIComponent(p[1]));
});


// ---------------------------------------------------------------- catalog
let catalogRows = [];
let pendingUpload = null;

async function loadCatalog() {
  const tbody = $('#catRows');
  if (!tbody) return;
  tbody.innerHTML = '';
  for (let i = 0; i < 4; i++) {
    const tr = el('tr', 'skel');
    for (let c = 0; c < 5; c++) {
      const td = el('td');
      td.appendChild(el('div', 'skeleton w' + (c === 1 ? '60' : '40')));
      tr.appendChild(td);
    }
    tbody.appendChild(tr);
  }
  try {
    const r = await fetchT('/api/catalog');
    if (!r.ok) throw new Error('HTTP ' + r.status);
    catalogRows = await r.json() || [];
  } catch (e) {
    tbody.innerHTML = '';
    const tr = el('tr');
    const td = el('td');
    td.colSpan = 5;
    td.appendChild(el('div', 'state-error', 'Cannot read the catalog: ' + e.message + '. Press Refresh.'));
    tr.appendChild(td);
    tbody.appendChild(tr);
    return;
  }
  renderCatalog();
}

function renderPager() {
  const pager = $('#catPager');
  if (!pager) return;
  pager.hidden = catTotal <= CAT_PAGE;
  if (pager.hidden) return;
  const from = catTotal === 0 ? 0 : catPage * CAT_PAGE + 1;
  const to = Math.min(catTotal, (catPage + 1) * CAT_PAGE);
  $('#catRange').textContent = from + '–' + to + ' of ' + catTotal;
  $('#catPrev').disabled = catPage === 0;
  $('#catNext').disabled = to >= catTotal;
}

function renderCatalog() {
  const tbody = $('#catRows');
  const q = ($('#catSearch')?.value || '').trim().toLowerCase();
  const kind = $('#catKind')?.value || '';
  const rows = catalogRows.filter(r =>
    (!kind || r.kind === kind) &&
    (!q || (r.name + ' ' + r.owner + ' ' + r.slug).toLowerCase().includes(q)));
  tbody.innerHTML = '';
  for (const r of rows) {
    const tr = el('tr');
    tr.tabIndex = 0;
    tr.addEventListener('click', () => openArtifact(r.slug));
    tr.addEventListener('keydown', (ev) => { if (ev.key === 'Enter') openArtifact(r.slug); });
    tr.appendChild(el('td', '', r.kind));
    tr.appendChild(el('td', '', r.name || r.slug));
    const status = el('td', '', r.status);
    status.dataset.status = r.status;
    tr.appendChild(status);
    tr.appendChild(el('td', '', 'v' + r.version));
    tr.appendChild(el('td', '', r.reach || '—'));
    tbody.appendChild(tr);
  }
  const empty = $('#catEmpty');
  empty.hidden = rows.length > 0;
  empty.textContent = catalogRows.length === 0
    ? 'No artifacts yet. Upload the first one, or let the next poll show what teams already use.'
    : 'No artifacts match this filter. Clear the search or choose another kind.';
  const drafts = catalogRows.filter(r => r.status === 'draft').length;
  const nav = document.querySelector('.surf[data-surface="catalog"]');
  if (nav) nav.textContent = drafts ? 'Catalog · ' + drafts : 'Catalog';
  if (nav) nav.title = drafts ? drafts + ' draft' + (drafts === 1 ? '' : 's') + ' waiting for approval' : '';
}

async function openArtifact(slug) {
  const box = $('#catDetail');
  box.hidden = false;
  box.innerHTML = 'Loading…';
  let d;
  try {
    const r = await fetchT('/api/catalog/' + encodeURIComponent(slug));
    if (!r.ok) throw new Error((await r.text()).trim() || ('HTTP ' + r.status));
    d = await r.json();
  } catch (e) {
    box.textContent = 'Cannot read ' + slug + ': ' + e.message;
    return;
  }
  history.replaceState(null, '', '#/catalog/' + encodeURIComponent(slug));
  box.innerHTML = '';
  const head = el('div', 'cat-head');
  const title = el('div');
  title.appendChild(el('h2', '', d.name || d.slug));
  title.appendChild(el('p', 'meta', [d.kind, d.owner, d.status, 'v' + d.version, (d.hash || '').slice(0, 10)].join(' · ')));
  head.appendChild(title);
  const close = el('button', 'act ghost', 'Close');
  close.addEventListener('click', () => { box.hidden = true; history.replaceState(null, '', '#/catalog'); });
  head.appendChild(close);
  box.appendChild(head);

  const msg = el('p', 'hint', '');
  const table = el('table', 'cat-versions');
  const thead = el('thead');
  const htr = el('tr');
  for (const label of ['Version', 'Status', 'Note', 'Created', 'Actions']) htr.appendChild(el('th', '', label));
  thead.appendChild(htr);
  table.appendChild(thead);
  const tb = el('tbody');
  const approvedVersion = (d.versions || []).find(v => v.status === 'approved')?.version || 0;
  for (const v of d.versions || []) {
    const tr = el('tr');
    tr.appendChild(el('td', '', 'v' + v.version));
    tr.appendChild(el('td', '', v.status));
    tr.appendChild(el('td', '', v.note || ''));
    const created = el('td', '', (v.createdAt || '').slice(0, 10));
    created.title = v.createdAt || '';
    tr.appendChild(created);
    const cell = el('td');
    const act = (label, action, confirmLabel) => {
      const b = el('button', 'act ghost', label);
      let armed = false;
      b.addEventListener('click', async (ev) => {
        ev.stopPropagation();
        if (confirmLabel && !armed) {
          armed = true;
          b.textContent = confirmLabel;
          setTimeout(() => { armed = false; b.textContent = label; }, 4000);
          return;
        }
        b.disabled = true;
        const res = await fetchT('/api/catalog/' + encodeURIComponent(d.slug) + '/status', {
          method: 'POST', headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({action, version: v.version}),
        });
        if (!res.ok) {
          msg.textContent = 'Not applied: ' + (await res.text()).trim();
          b.disabled = false;
          return;
        }
        await loadCatalog();
        openArtifact(d.slug);
      });
      cell.appendChild(b);
    };
    if (v.status !== 'approved') act('Approve', 'approve', 'Confirm approve');
    if (v.status === 'approved' && v.version !== approvedVersion) act('Roll back', 'rollback', 'Confirm rollback');
    if (v.status === 'approved') act('Retire', 'retire', 'Confirm retire');
    tr.appendChild(cell);
    tb.appendChild(tr);
  }
  table.appendChild(tb);
  box.appendChild(table);
  box.appendChild(msg);
  box.appendChild(el('pre', '', d.body || '(file not readable)'));
}

function wireCatalog() {
  $('#catSearch')?.addEventListener('input', renderCatalog);
  $('#catKind')?.addEventListener('change', renderCatalog);
  $('#catNew')?.addEventListener('click', () => {
    const form = $('#catUpload');
    form.hidden = !form.hidden;
    $('#catDetail').hidden = true;
    if (!form.hidden) {
      form.reset();
      pendingUpload = null;
      $('#upProblems').hidden = true;
      $('#upDiff').hidden = true;
      $('#upStatus').textContent = '';
      $('#upPick').textContent = 'Choose file';
      $('#upFileName').textContent = 'no file chosen';
      $('#upSlug').focus();
    }
  });
  $('#upCancel')?.addEventListener('click', () => { $('#catUpload').hidden = true; pendingUpload = null; });
  $('#upPick')?.addEventListener('click', () => $('#upFile').click());
  $('#upFile')?.addEventListener('change', () => {
    const f = $('#upFile').files[0];
    $('#upFileName').textContent = f ? f.name + ' · ' + Math.round(f.size / 1024) + ' KB' : 'no file chosen';
    pendingUpload = null;
    $('#upDiff').hidden = true;
  });
  $('#catUpload')?.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    const form = ev.currentTarget;
    const status = $('#upStatus');
    const problemsBox = $('#upProblems');
    const diffBox = $('#upDiff');
    const data = new FormData(form);
    if (!data.get('file') || !data.get('file').size) { status.textContent = 'Choose a file first.'; return; }
    problemsBox.hidden = true;
    if (!pendingUpload) {
      status.textContent = 'Validating…';
      const res = await fetchT('/api/catalog/validate', {method: 'POST', body: data});
      if (!res.ok) { status.textContent = (await res.text()).trim(); return; }
      const out = await res.json();
      if ((out.problems || []).length) {
        problemsBox.innerHTML = '';
        problemsBox.appendChild(el('strong', '', 'Not saved:'));
        const ul = el('ul');
        for (const p of out.problems) ul.appendChild(el('li', '', p));
        problemsBox.appendChild(ul);
        problemsBox.hidden = false;
        status.textContent = '';
        return;
      }
      pendingUpload = data;
      diffBox.innerHTML = '';
      diffBox.appendChild(el('strong', '', out.diff ? 'Changes against the approved version' : 'First version of this artifact'));
      diffBox.appendChild(el('p', '', out.diff || 'Nothing to compare; approval will publish it.'));
      diffBox.hidden = false;
      $('#upSave').textContent = 'Save as draft v' + out.version;
      status.textContent = 'Reviewed. Save, then approve in the list.';
      return;
    }
    status.textContent = 'Saving…';
    const res = await fetchT('/api/catalog', {method: 'POST', body: pendingUpload});
    if (!res.ok) { status.textContent = (await res.text()).trim(); return; }
    const d = await res.json();
    pendingUpload = null;
    status.textContent = 'Saved as draft v' + d.version + '. Approve it in the list below.';
    form.reset();
    form.hidden = true;
    $('#upSave').textContent = 'Validate and review';
    $('#upDiff').hidden = true;
    await loadCatalog();
    openArtifact(d.slug);
  });
}

wireCatalog();


// ---------------------------------------------------------------- teams
async function loadTeams() {
  const facts = $('#teamFacts');
  if (!facts) return;
  let t;
  try {
    const r = await fetchT('/api/teams');
    if (!r.ok) throw new Error('HTTP ' + r.status);
    t = await r.json();
  } catch (e) {
    facts.innerHTML = '';
    facts.appendChild(factCell('Directory', 'unreadable', 'bad'));
    $('#teamStatus').textContent = e.message;
    return;
  }
  facts.innerHTML = '';
  facts.appendChild(factCell('Directory', t.loaded ? t.rows + ' people · ' + t.teams + ' teams' : 'not loaded', t.loaded ? 'ok' : ''));
  facts.appendChild(factCell('Sessions mapped · 7d', t.loaded ? Math.round(t.coveragePct) + '%' : '—', t.loaded && t.coveragePct > 0 ? 'ok' : ''));
  facts.appendChild(factCell('Unassigned', t.loaded ? String(t.unassigned) : '—', t.loaded && t.unassigned > 0 ? 'bad' : ''));
  facts.appendChild(factCell('Hidden teams', String(t.hiddenTeams || 0), t.hiddenTeams ? 'bad' : ''));

  const body = $('#teamRows');
  body.innerHTML = '';
  for (const row of t.teamRows || []) {
    const tr = el('tr');
    tr.appendChild(el('td', '', row.team));
    tr.appendChild(el('td', '', row.people ? String(row.people) : '—'));
    tr.appendChild(el('td', '', String(row.sessions)));
    body.appendChild(tr);
  }
  if (!(t.teamRows || []).length) {
    const tr = el('tr');
    const td = el('td');
    td.colSpan = 3;
    td.appendChild(el('div', 'state-empty', 'No sessions yet, so there is nothing to attribute. Load a directory and let the collector run.'));
    tr.appendChild(td);
    body.appendChild(tr);
  }
  $('#teamEmpty').hidden = true;
  $('#teamHint').textContent = t.loaded
    ? 'Loaded ' + (t.loadedAt || '').slice(0, 16) + ' · ' + t.rows + ' people, ' + t.teams + ' teams. The file itself is not stored.'
    : 'Paste email,team rows, one person per line. Loading replaces the current mapping; the previous batch stays for one generation.';
}

async function sendTeams(apply) {
  const status = $('#teamStatus');
  const problems = $('#teamProblems');
  problems.hidden = true;
  const csv = $('#teamCSV').value;
  if (!csv.trim()) { status.textContent = 'Paste the rows first.'; return; }
  status.textContent = apply ? 'Hashing and loading…' : 'Reading…';
  const r = await fetchT('/api/teams', {
    method: 'POST', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({csv, apply}),
  });
  const out = await r.json().catch(() => ({}));
  if (r.status === 422 || (out.preview && out.preview.malformed && out.preview.malformed.length && !apply)) {
    problems.innerHTML = '';
    problems.appendChild(el('strong', '', apply ? 'Nothing was written.' : 'Lines that will be refused:'));
    const ul = el('ul');
    for (const p of out.preview.malformed) ul.appendChild(el('li', '', 'line ' + p.line + ': ' + p.reason));
    problems.appendChild(ul);
    problems.hidden = false;
    status.textContent = apply ? (out.problem || 'Fix the lines above.') : out.preview.unique + ' unique · ' + out.preview.teams + ' teams · ' + out.belowFloor + ' below the floor';
    return;
  }
  if (!r.ok) { status.textContent = (out && out.problem) || 'Request failed.'; return; }
  if (!apply) {
    status.textContent = out.preview.rows + ' rows · ' + out.preview.unique + ' unique · ' + out.preview.duplicates + ' duplicates · ' + out.preview.teams + ' teams · ' + out.belowFloor + ' below the floor';
    return;
  }
  status.textContent = 'Loaded. ' + (out.rows || 0) + ' people across ' + (out.teams || 0) + ' teams.';
  $('#teamCSV').value = '';
  loadTeams();
}

function wireTeams() {
  $('#teamPick')?.addEventListener('click', () => $('#teamFile').click());
  $('#teamFile')?.addEventListener('change', async () => {
    const f = $('#teamFile').files[0];
    if (!f) return;
    $('#teamFileName').textContent = f.name + ' · ' + Math.round(f.size / 1024) + ' KB';
    if (f.size > 2 << 20) { $('#teamStatus').textContent = 'That file is larger than 2 MB; the loader refuses it.'; return; }
    $('#teamCSV').value = await f.text();
  });
  $('#teamPreview')?.addEventListener('click', () => sendTeams(false));
  $('#teamApply')?.addEventListener('click', () => sendTeams(true));
}
wireTeams();

// ---------------------------------------------------------------- coach
// One person's figures against their own previous window (ADR-0032, ADR-0048).
function coachCell(label, value, prev) {
  const c = document.createElement('div');
  c.className = 'coach-cell';
  c.appendChild(Object.assign(document.createElement('b'), { textContent: value }));
  const l = document.createElement('span');
  l.textContent = label;
  c.appendChild(l);
  if (prev) c.appendChild(Object.assign(document.createElement('i'), { textContent: prev }));
  return c;
}

function renderCoach(v, standalone) {
  const box = $('#coachBody');
  if (!box) return;
  box.innerHTML = '';
  box.hidden = false;

  const head = document.createElement('div');
  head.className = 'coach-head';
  const h2 = document.createElement('h2');
  h2.textContent = v.pseudonym.slice(0, 12) + '…';
  head.appendChild(h2);
  const win = document.createElement('span');
  win.textContent = v.from + ' → ' + v.to;
  head.appendChild(win);
  box.appendChild(head);

  if (!v.report.now.sessions) {
    const p = document.createElement('p');
    p.className = 'coach-empty';
    p.textContent = v.unidentified
      ? v.unidentified + ' sessions arrived without an identity, so no view can attribute them. Claude Code omits user.email when a run authenticates with an API key; sign in with your Claude account for these, or label the run as a service.'
      : 'No sessions for this pseudonym in this window. Run Claude Code once in an instrumented project, then come back.';
    box.appendChild(p);
    return;
  }

  const hero = document.createElement('div');
  hero.className = 'coach-hero';
  const n = v.report.now, pv = v.report.prev;
  hero.appendChild(coachCell('Sessions', String(n.sessions), 'prev ' + pv.sessions));
  hero.appendChild(coachCell('Prompts', String(n.prompts), 'prev ' + pv.prompts));
  hero.appendChild(coachCell('Cost', '$' + n.costUsd.toFixed(2), 'prev $' + pv.costUsd.toFixed(2)));
  hero.appendChild(coachCell('Peak context', n.peakContext.toLocaleString(), 'prev ' + pv.peakContext.toLocaleString()));
  box.appendChild(hero);

  if (v.weekly && v.weekly.length) {
    const max = Math.max(1, ...v.weekly.map(w => w.sessions));
    const spark = document.createElement('div');
    spark.className = 'spark';
    for (const w of v.weekly) {
      const bar = document.createElement('div');
      bar.style.height = Math.max(2, Math.round((w.sessions / max) * 64)) + 'px';
      bar.title = w.week.slice(0, 10) + ' · ' + w.sessions + ' sessions · $' + w.costUsd.toFixed(2);
      const lbl = document.createElement('span');
      lbl.textContent = w.week.slice(5, 10);
      bar.appendChild(lbl);
      spark.appendChild(bar);
    }
    box.appendChild(spark);
  }

  const sug = document.createElement('ol');
  sug.className = 'coach-sug';
  for (const s of v.report.suggestions) {
    const li = document.createElement('li');
    const b = document.createElement('b');
    b.textContent = String(sug.children.length + 1).padStart(2, '0');
    li.appendChild(b);
    li.appendChild(document.createTextNode(s));
    sug.appendChild(li);
  }
  if (!v.report.suggestions.length) {
    const li = document.createElement('li');
    li.textContent = 'No suggestion this window: nothing your colleagues have adopted is missing from your own work.';
    sug.appendChild(li);
  }
  box.appendChild(sug);

  if (!standalone) {
    const row = document.createElement('div');
    row.className = 'coach-link';
    const btn = document.createElement('button');
    btn.className = 'act';
    btn.textContent = 'Copy link for this person';
    const out = document.createElement('span');
    out.className = 'hint';
    out.textContent = 'One link, only this pseudonym; rotating the board token revokes it.';
    btn.addEventListener('click', async () => {
      try {
        const r = await fetchT('/api/coach/link', {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ pseudonym: v.pseudonym })
        });
        if (!r.ok) throw new Error(await r.text());
        const j = await r.json();
        out.textContent = location.origin + j.url;
        try { await navigator.clipboard.writeText(location.origin + j.url); } catch (e) {}
      } catch (e) { out.textContent = 'Could not issue a link: ' + e.message; }
    });
    row.appendChild(btn);
    row.appendChild(out);
    box.appendChild(row);
  }
}

async function loadCoach(pseudonym, key) {
  const box = $('#coachBody');
  if (box) { box.hidden = false; box.innerHTML = '<p class="hint">Reading the figures…</p>'; }
  const url = key
    ? '/api/coach/open?pseudonym=' + encodeURIComponent(pseudonym) + '&k=' + encodeURIComponent(key)
    : '/api/coach?pseudonym=' + encodeURIComponent(pseudonym);
  try {
    const r = await fetchT(url, null, 20000);
    const text = await r.text();
    if (!r.ok) throw new Error(text.trim() || ('HTTP ' + r.status));
    renderCoach(JSON.parse(text), Boolean(key));
  } catch (e) {
    if (box) { box.innerHTML = ''; const p = document.createElement('p'); p.className = 'state-error'; p.textContent = 'Cannot read the coach: ' + e.message; box.appendChild(p); }
  }
}

async function coachPreview(email) {
  const box = $('#coachBody');
  if (box) { box.hidden = false; box.innerHTML = '<p class="hint">Resolving the pseudonym…</p>'; }
  try {
    const r = await fetchT('/api/coach/resolve?email=' + encodeURIComponent(email));
    const text = await r.text();
    if (!r.ok) throw new Error(text.trim() || ('HTTP ' + r.status));
    const j = JSON.parse(text);
    await loadCoach(j.pseudonym, '');
  } catch (e) {
    if (box) { box.innerHTML = ''; const p = document.createElement('p'); p.className = 'state-error'; p.textContent = 'Cannot resolve that address: ' + e.message; box.appendChild(p); }
  }
}

$('#coachForm')?.addEventListener('submit', ev => {
  ev.preventDefault();
  const email = $('#coachEmail').value.trim();
  if (email) coachPreview(email);
});

// A per-person link: same shell, no navigation, no other surfaces.
(() => {
  const m = location.pathname.match(/^\/coach\/([^/]+)/);
  if (!m) return;
  document.body.classList.add('coach-only');
  const key = new URLSearchParams(location.search).get('k') || '';
  showSurface('coach', { fromHash: true });
  loadCoach(decodeURIComponent(m[1]), key);
})();
