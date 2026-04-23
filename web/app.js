// Belayer Web UI — Live Agent Dashboard
// Vanilla JS, no build step. Consumes the Belayer daemon REST API + SSE.
// Supports both single-daemon mode and multi-daemon dashboard mode.

(function () {
  'use strict';

  // --- Config ---
  const API_BASE = '';
  const POLL_INTERVAL_MS = 5000;
  const OUTLINE_POLL_MS = 10000;
  const MAX_BACKOFF_MS = 30000;

  // --- State ---
  let token = '';
  let dashboardMode = false;
  let daemons = [];              // in dashboard mode: [{name, url, healthy, sessions}]
  let sessions = [];
  let activeSessionId = null;
  let activeDaemon = null;       // name of daemon owning active session
  let events = [];
  let messages = [];
  let toolCalls = [];
  let agents = new Map();
  let artifacts = [];
  let sessionMeta = null;
  let sseController = null;
  let reconnectTimer = null;
  let backoffMs = 500;
  let lastEventId = 0;
  let daemonInstanceId = null;
  let sessionPollTimer = null;
  let outlinePollTimer = null;
  let autoScroll = true;
  let activeTab = 'events';
  let typeFilters = new Set(['session_', 'agent_', 'bridge:', 'message_', 'artifact_', 'tool_', 'completion_', 'pm_', 'warning:', 'node_', 'trace:', 'custom_event']);
  let agentFilters = new Set();

  // --- Agent Colors ---
  const AGENT_COLORS = {
    supervisor: '#8b5cf6',
    'backend-dev': '#22c55e',
    'web-dev': '#f59e0b',
    qa: '#ef4444',
    pm: '#06b6d4',
    reviewer: '#94a3b8',
    system: '#64748b',
    unknown: '#a855f7',
  };

  function agentColor(name) {
    if (!name) return AGENT_COLORS.system;
    const base = name.split(/[.-]/)[0].toLowerCase();
    return AGENT_COLORS[base] || AGENT_COLORS.unknown;
  }

  function agentPill(name) {
    const color = agentColor(name);
    const display = name || 'system';
    return `<span class="agent-pill" style="border-left-color:${color};background:${color}26"><span class="agent-dot" style="background:${color}"></span>${escapeHtml(display)}</span>`;
  }

  // --- Utils ---
  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  }

  function fmtTime(iso) {
    const d = new Date(iso);
    return d.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
  }

  function fmtRelTime(iso) {
    const diff = (Date.now() - new Date(iso).getTime()) / 1000;
    if (diff < 60) return 'now';
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
    if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
    return `${Math.floor(diff / 86400)}d ago`;
  }

  function syntaxHighlight(json) {
    if (!json) return '';
    const s = typeof json === 'string' ? json : JSON.stringify(json, null, 2);
    return escapeHtml(s)
      .replace(/"(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)/g, '<span class="json-key">$1</span>$2')
      .replace(/"(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"/g, '<span class="json-string">$&</span>')
      .replace(/\b(true|false)\b/g, '<span class="json-boolean">$1</span>')
      .replace(/\b(null)\b/g, '<span class="json-null">$1</span>')
      .replace(/\b(\d+\.?\d*)\b/g, '<span class="json-number">$1</span>');
  }

  // --- API ---
  function apiUrl(path, daemonName) {
    let base = '';
    if (dashboardMode && daemonName) {
      base = `/api/daemons/${encodeURIComponent(daemonName)}`;
    }
    const sep = path.includes('?') ? '&' : '?';
    return `${API_BASE}${base}${path}${token ? sep + 'token=' + encodeURIComponent(token) : ''}`;
  }

  async function apiGet(path, daemonName) {
    let url = API_BASE + path;
    if (dashboardMode && daemonName) {
      url = `${API_BASE}/api/daemons/${encodeURIComponent(daemonName)}${path}`;
    }
    const headers = {};
    if (token) headers['Authorization'] = 'Bearer ' + token;
    const res = await fetch(url, { headers });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.json();
  }

  async function apiGetText(path, daemonName) {
    let url = API_BASE + path;
    if (dashboardMode && daemonName) {
      url = `${API_BASE}/api/daemons/${encodeURIComponent(daemonName)}${path}`;
    }
    const headers = {};
    if (token) headers['Authorization'] = 'Bearer ' + token;
    const res = await fetch(url, { headers });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.text();
  }

  // --- Init ---
  async function init() {
    const params = new URLSearchParams(window.location.search);
    token = params.get('token') || '';

    // Detect dashboard mode
    try {
      const dlist = await apiGet('/api/daemons');
      if (Array.isArray(dlist)) {
        dashboardMode = true;
        daemons = dlist.map(d => ({ ...d, sessions: [] }));
        document.querySelector('.brand').textContent = 'Dashboard';
      }
    } catch (e) {
      // 404 or error -> single-daemon mode
      dashboardMode = false;
    }

    if (!dashboardMode) {
      checkHealth();
    }
    loadSessions();
    bindTabs();
    bindFilters();
    bindScroll();

    sessionPollTimer = setInterval(loadSessions, POLL_INTERVAL_MS);

    // Collapsible filters
    const filtersToggle = document.getElementById('filters-toggle');
    if (filtersToggle) {
      filtersToggle.addEventListener('click', () => {
        const body = document.getElementById('filters-body');
        body.classList.toggle('collapsed');
        document.querySelector('#filters-toggle .chevron').textContent = body.classList.contains('collapsed') ? '▶' : '▼';
      });
    }
  }

  async function checkHealth() {
    try {
      const data = await apiGet('/health');
      document.getElementById('daemon-id').textContent = data.daemon_instance_id?.slice(0, 8) || '—';
      if (!data.capabilities?.web_ui) {
        showBanner('Daemon does not advertise web_ui capability — UI may not work.', 'warn');
      }
    } catch (e) {
      showBanner('Daemon health check failed: ' + e.message, 'danger');
    }
  }

  // --- Sessions ---
  async function loadSessions() {
    if (dashboardMode) {
      try {
        const dlist = await apiGet('/api/daemons');
        if (!Array.isArray(dlist)) return;
        daemons = dlist.map(d => ({ ...d, sessions: [] }));

        await Promise.all(daemons.map(async (d) => {
          try {
            const data = await apiGet('/sessions', d.name);
            d.sessions = (data || []).map(s => ({ ...s, _daemon: d.name }));
          } catch (e) {
            d.sessions = [];
          }
        }));

        // Flatten for backward-compatible session list
        sessions = daemons.flatMap(d => d.sessions);
        renderSessions();
      } catch (e) {
        console.error('loadSessions dashboard', e);
      }
      return;
    }

    try {
      const data = await apiGet('/sessions');
      sessions = data || [];
      renderSessions();
    } catch (e) {
      console.error('loadSessions', e);
    }
  }

  function renderSessions() {
    const list = document.getElementById('sessions-list');
    if (!list) return;

    if (dashboardMode) {
      list.innerHTML = daemons.map(d => `
        <li class="daemon-group">
          <div class="daemon-header">
            <span class="daemon-dot ${d.healthy ? 'healthy' : 'unhealthy'}"></span>
            <span class="daemon-name">${escapeHtml(d.name)}</span>
          </div>
          <ul class="daemon-sessions">
            ${d.sessions.map(s => `
              <li class="session-item ${s.id === activeSessionId ? 'active' : ''}" data-id="${escapeHtml(s.id)}" data-daemon="${escapeHtml(d.name)}">
                <div class="session-name">${escapeHtml(s.name || s.id.slice(0, 8))}</div>
                <div class="session-meta">
                  <span class="status-badge ${s.status}">${escapeHtml(s.status)}</span>
                  ${fmtRelTime(s.created_at)}
                </div>
              </li>
            `).join('')}
          </ul>
        </li>
      `).join('');

      list.querySelectorAll('.session-item').forEach(el => {
        el.addEventListener('click', () => selectSession(el.dataset.id, el.dataset.daemon));
      });
      return;
    }

    list.innerHTML = sessions.map(s => `
      <li class="session-item ${s.id === activeSessionId ? 'active' : ''}" data-id="${escapeHtml(s.id)}">
        <div class="session-name">${escapeHtml(s.name || s.id.slice(0, 8))}</div>
        <div class="session-meta">
          <span class="status-badge ${s.status}">${escapeHtml(s.status)}</span>
          ${fmtRelTime(s.created_at)}
        </div>
      </li>
    `).join('');
    list.querySelectorAll('.session-item').forEach(el => {
      el.addEventListener('click', () => selectSession(el.dataset.id));
    });
  }

  async function selectSession(id, daemonName) {
    if (activeSessionId === id && (!dashboardMode || activeDaemon === daemonName)) return;
    activeSessionId = id;
    if (dashboardMode && daemonName) {
      activeDaemon = daemonName;
    }
    events = [];
    messages = [];
    toolCalls = [];
    lastEventId = 0;
    agents.clear();
    artifacts = [];
    renderSessions();
    closeSSE();

    const currentDaemon = dashboardMode ? activeDaemon : null;

    try {
      const session = await apiGet(`/sessions/${id}`, currentDaemon);
      sessionMeta = session;
      const displayName = session.name || id.slice(0, 8);
      const daemonLabel = dashboardMode && currentDaemon ? `[${currentDaemon}] ` : '';
      document.getElementById('active-session-name').textContent = daemonLabel + displayName;
      document.getElementById('active-session-status').textContent = session.status;
      document.getElementById('active-session-status').className = 'status-badge ' + session.status;
      document.getElementById('active-session-status').classList.remove('hidden');

      if (session.agents) {
        session.agents.forEach(a => agents.set(a.name, a));
      }
      renderRoster();
      renderMeta();
      renderAgentFilters();

      const arts = await apiGet(`/sessions/${id}/artifacts`, currentDaemon);
      artifacts = arts || [];
      renderArtifacts();

      const hist = await apiGet(`/sessions/${id}/events?after=0&limit=1000`, currentDaemon);
      if (hist && hist.length) {
        events = hist;
        lastEventId = hist[hist.length - 1].id;
        processEvents(hist);
      }
      renderEvents();
      renderMessages();

      const tools = await apiGet(`/sessions/${id}/tool-calls`, currentDaemon);
      toolCalls = tools || [];
      renderToolCalls();

      openSSE(id, currentDaemon);
      startOutlinePoll(currentDaemon);
    } catch (e) {
      showBanner('Failed to load session: ' + e.message, 'danger');
    }
  }

  // --- SSE ---
  function openSSE(sessionId, daemonName) {
    closeSSE();
    setSSEStatus('reconnecting');

    const url = apiUrl(`/events/stream?sessions=${encodeURIComponent(sessionId)}&tier=verbose&after=${lastEventId}`, daemonName);
    const headers = {};
    if (token) headers['Authorization'] = 'Bearer ' + token;

    fetch(url, { headers })
      .then(res => {
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
        setSSEStatus('connected');
        backoffMs = 500;
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';

        sseController = { abort: () => reader.cancel() };

        function pump() {
          return reader.read().then(({ done, value }) => {
            if (done) {
              scheduleReconnect(sessionId, daemonName);
              return;
            }
            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split('\n');
            buffer = lines.pop();
            parseSSELines(lines);
            return pump();
          }).catch(err => {
            console.error('SSE read error', err);
            scheduleReconnect(sessionId, daemonName);
          });
        }
        return pump();
      })
      .catch(err => {
        console.error('SSE fetch error', err);
        setSSEStatus('disconnected');
        scheduleReconnect(sessionId, daemonName);
      });
  }

  function closeSSE() {
    if (sseController) {
      try { sseController.abort(); } catch (e) {}
      sseController = null;
    }
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    stopOutlinePoll();
  }

  function scheduleReconnect(sessionId, daemonName) {
    setSSEStatus('reconnecting');
    reconnectTimer = setTimeout(() => {
      backoffMs = Math.min(backoffMs * 2, MAX_BACKOFF_MS);
      openSSE(sessionId, daemonName);
    }, backoffMs);
  }

  function parseSSELines(lines) {
    let current = {};
    for (const line of lines) {
      if (line.startsWith('id:')) {
        current.id = parseInt(line.slice(3).trim(), 10);
      } else if (line.startsWith('event:')) {
        current.event = line.slice(6).trim();
      } else if (line.startsWith('data:')) {
        current.data = (current.data || '') + line.slice(5).trim();
      } else if (line === '' && current.event) {
        handleSSEEvent(current);
        current = {};
      }
    }
  }

  function handleSSEEvent(frame) {
    if (frame.event === 'daemon_hello') {
      try {
        const data = JSON.parse(frame.data);
        if (daemonInstanceId && daemonInstanceId !== data.daemon_instance_id) {
          events = [];
          lastEventId = 0;
          daemonInstanceId = data.daemon_instance_id;
          if (activeSessionId) openSSE(activeSessionId, activeDaemon);
          return;
        }
        daemonInstanceId = data.daemon_instance_id;
      } catch (e) {}
      return;
    }
    if (frame.event === 'daemon_draining') {
      showBanner('Daemon is draining — sessions will archive soon', 'warn');
      return;
    }
    if (frame.event === 'session_digest') {
      return;
    }
    if (!frame.event || frame.id == null) return;

    const evt = { type: frame.event, id: frame.id, data: {} };
    try { evt.data = JSON.parse(frame.data); } catch (e) {}
    if (evt.id > lastEventId) lastEventId = evt.id;
    events.push(evt);
    processEvent(evt);
    renderEvents();
    renderMessages();
  }

  function processEvents(list) {
    for (const e of list) processEvent(e);
  }

  function processEvent(e) {
    if (e.type === 'message_sent' || e.type === 'message_broadcast') {
      messages.push(e);
    }
    const agentName = e.data?.agent || e.data?.from || e.data?.agent_name;
    if (agentName && !agents.has(agentName)) {
      agents.set(agentName, { name: agentName, role: 'unknown', status: 'running' });
    }
  }

  // --- Outline Polling ---
  function startOutlinePoll(daemonName) {
    stopOutlinePoll();
    if (!activeSessionId) return;
    outlinePollTimer = setInterval(async () => {
      try {
        const data = await apiGet(`/sessions/${activeSessionId}/outline`, daemonName);
        if (data.agents) {
          data.agents.forEach(a => agents.set(a.name, a));
          renderRoster();
        }
        if (data.artifacts) {
          artifacts = data.artifacts;
          renderArtifacts();
        }
        if (data.session) {
          sessionMeta = { ...sessionMeta, ...data.session };
          renderMeta();
        }
      } catch (e) {
        console.error('outline poll', e);
      }
    }, OUTLINE_POLL_MS);
  }

  function stopOutlinePoll() {
    if (outlinePollTimer) { clearInterval(outlinePollTimer); outlinePollTimer = null; }
  }

  // --- Rendering ---
  function renderEvents() {
    const feed = document.getElementById('events-feed');
    if (!feed) return;
    const filtered = events.filter(e => {
      const typeOk = Array.from(typeFilters).some(p => e.type.startsWith(p));
      const agent = e.data?.agent || e.data?.from || e.data?.agent_name || 'system';
      const agentOk = agentFilters.size === 0 || agentFilters.has(agent);
      return typeOk && agentOk;
    });

    feed.innerHTML = filtered.map(e => `
      <div class="event-row" data-id="${e.id}">
        <div class="event-header">
          <span class="event-ts">${fmtTime(e.timestamp || e.data?.sent_at)}</span>
          ${agentPill(e.data?.agent || e.data?.from || e.data?.agent_name)}
          <span class="event-type">${escapeHtml(e.type)}</span>
        </div>
        <div class="event-payload">${syntaxHighlight(e.data)}</div>
      </div>
    `).join('');

    feed.querySelectorAll('.event-row').forEach(row => {
      row.addEventListener('click', () => row.classList.toggle('expanded'));
    });

    if (autoScroll && activeTab === 'events') {
      const container = document.getElementById('tab-events');
      container.scrollTop = container.scrollHeight;
    }
  }

  function renderMessages() {
    const feed = document.getElementById('messages-feed');
    if (!feed) return;
    const list = messages.filter(m => {
      const agent = m.data?.from || m.data?.agent || 'system';
      return agentFilters.size === 0 || agentFilters.has(agent);
    });

    if (!list.length) {
      feed.innerHTML = '<div class="empty-state">No messages yet</div>';
      return;
    }

    feed.innerHTML = list.map(m => `
      <div class="message-row">
        <div class="message-header">
          ${agentPill(m.data?.from)}
          <span class="message-arrow">→</span>
          <span class="message-to">${escapeHtml(m.data?.to || 'broadcast')}</span>
          <span class="event-ts">${fmtTime(m.data?.sent_at || m.timestamp)}</span>
        </div>
        <div class="message-body">${escapeHtml(m.data?.content || '')}</div>
      </div>
    `).join('');
  }

  function renderToolCalls() {
    const feed = document.getElementById('toolcalls-feed');
    if (!feed) return;
    if (!toolCalls.length) {
      feed.innerHTML = '<div class="empty-state">No tool calls yet</div>';
      return;
    }
    feed.innerHTML = toolCalls.map(t => `
      <div class="tool-row">
        <div class="tool-header">
          <span class="tool-name">${escapeHtml(t.tool)}</span>
          ${agentPill(t.agent)}
          <span class="tool-duration">${t.duration_ms}ms</span>
          <span class="tool-status ${t.status}">${escapeHtml(t.status)}</span>
        </div>
        <div class="tool-body">
${escapeHtml(JSON.stringify(t, null, 2))}
        </div>
      </div>
    `).join('');

    feed.querySelectorAll('.tool-header').forEach(h => {
      h.addEventListener('click', () => h.closest('.tool-row').classList.toggle('expanded'));
    });
  }

  function renderRoster() {
    const list = document.getElementById('agent-roster');
    if (!list) return;
    const items = Array.from(agents.values());
    if (!items.length) {
      list.innerHTML = '<li class="empty-state">No agents</li>';
      return;
    }
    list.innerHTML = items.map(a => `
      <li class="roster-item">
        <span class="agent-dot" style="background:${agentColor(a.name)}"></span>
        <div>
          <div class="roster-name">${escapeHtml(a.name)}</div>
          <div class="roster-role">${escapeHtml(a.role || 'unknown')}</div>
        </div>
        <span class="roster-status ${a.status || 'pending'}">${escapeHtml(a.status || 'pending')}</span>
      </li>
    `).join('');
  }

  function renderMeta() {
    if (!sessionMeta) return;
    const dl = document.getElementById('session-meta');
    if (!dl) return;
    dl.innerHTML = `
      <dt>ID</dt><dd>${escapeHtml(sessionMeta.id || '—')}</dd>
      <dt>Status</dt><dd>${escapeHtml(sessionMeta.status || '—')}</dd>
      <dt>Log Level</dt><dd>${escapeHtml(sessionMeta.log_level || '—')}</dd>
      <dt>Created</dt><dd>${sessionMeta.created_at ? fmtTime(sessionMeta.created_at) : '—'}</dd>
      <dt>Phase</dt><dd>${escapeHtml(sessionMeta.phase || '—')}</dd>
    `;
  }

  function renderArtifacts() {
    const list = document.getElementById('artifacts-list');
    if (!list) return;
    if (!artifacts.length) {
      list.innerHTML = '<li class="empty-state">No artifacts</li>';
      return;
    }
    list.innerHTML = artifacts.map(a => `
      <li class="artifact-item" data-id="${escapeHtml(a.id)}">
        <div class="artifact-kind">${escapeHtml(a.kind || 'artifact')}</div>
        <div class="artifact-path">${escapeHtml(a.path || a.id)}</div>
      </li>
    `).join('');
  }

  function renderAgentFilters() {
    const container = document.getElementById('agent-filters');
    if (!container) return;
    const names = Array.from(agents.keys());
    if (!names.length) {
      container.innerHTML = '<h4>Agents</h4><div class="filter-chips"><span class="chip">all</span></div>';
      return;
    }
    container.innerHTML = '<h4>Agents</h4><div class="filter-chips">' +
      names.map(n => `<span class="chip ${agentFilters.has(n) ? 'active' : ''}" data-agent="${escapeHtml(n)}">${escapeHtml(n)}</span>`).join('') +
      '</div>';
    container.querySelectorAll('.chip').forEach(ch => {
      ch.addEventListener('click', () => {
        const a = ch.dataset.agent;
        if (agentFilters.has(a)) agentFilters.delete(a);
        else agentFilters.add(a);
        renderAgentFilters();
        renderEvents();
        renderMessages();
      });
    });
  }

  // --- Tabs ---
  function bindTabs() {
    document.querySelectorAll('.tab').forEach(tab => {
      tab.addEventListener('click', () => {
        document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
        document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
        tab.classList.add('active');
        document.getElementById('tab-' + tab.dataset.tab).classList.add('active');
        activeTab = tab.dataset.tab;
      });
    });
  }

  // --- Filters ---
  function bindFilters() {
    document.querySelectorAll('.filter-chips .chip[data-filter]').forEach(chip => {
      chip.addEventListener('click', () => {
        chip.classList.toggle('active');
        const filter = chip.dataset.filter;
        if (chip.classList.contains('active')) typeFilters.add(filter);
        else typeFilters.delete(filter);
        renderEvents();
      });
    });
  }

  // --- Scroll ---
  function bindScroll() {
    const container = document.getElementById('tab-events');
    if (!container) return;
    container.addEventListener('scroll', () => {
      const nearBottom = container.scrollHeight - container.scrollTop - container.clientHeight < 50;
      autoScroll = nearBottom;
    });
  }

  // --- UI helpers ---
  function setSSEStatus(status) {
    const el = document.getElementById('sse-status');
    if (!el) return;
    el.className = 'sse-dot ' + status;
    el.textContent = status === 'connected' ? 'Live' : status === 'reconnecting' ? 'Reconnecting…' : 'Offline';

    const dot = document.getElementById('conn-status');
    if (dot) {
      dot.className = 'sse-dot ' + status;
    }
  }

  function showBanner(text, severity) {
    const b = document.getElementById('draining-banner');
    if (!b) return;
    b.textContent = text;
    b.className = 'banner show ' + severity;
    setTimeout(() => b.classList.remove('show'), 10000);
  }

  // --- Start ---
  init();
})();
