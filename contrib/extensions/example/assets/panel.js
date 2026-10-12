/**
 * @param {HTMLElement} root
 * @param {import('../../../../sdk/browser/extension-contract.js').MountContext} context
 * @returns {import('../../../../sdk/browser/extension-contract.js').MountHandle}
 */
export function mount(root, context) {
  if (context.contractVersion !== 1) throw new TypeError('Unsupported browser contract version');
  const {request, stream} = context;
  root.classList.add('ui', 'form');
  const label = document.createElement('label');
  const field = document.createElement('div');
  field.className = 'field';
  const input = document.createElement('textarea');
  input.id = 'example-persistent-notes';
  input.dataset.persistentNotes = '';
  input.rows = 8;
  input.style.width = '100%';
  input.disabled = true;
  label.htmlFor = input.id;
  label.textContent = 'Notes stay mounted while Forgejo pages navigate';

  const save = document.createElement('button');
  save.type = 'button';
  save.className = 'ui primary button';
  save.textContent = 'Save notes';
  save.disabled = true;
  const status = document.createElement('p');
  status.setAttribute('role', 'status');
  status.textContent = 'Loading notes…';

  const lifetime = document.createElement('p');
  lifetime.dataset.panelLifetime = '';
  const started = Date.now();
  const updateLifetime = () => {
    lifetime.textContent = `Mounted for ${Math.floor((Date.now() - started) / 1000)} seconds`;
  };
  /** @type {number | undefined} */
  let timer;
  const controller = new AbortController();
  let disposed = false;
  let visible = true;
  let suspended = false;
  const syncVisibleWork = () => {
    root.hidden = disposed || suspended || !visible;
    if (root.hidden) {
      window.clearInterval(timer);
      timer = undefined;
    } else if (timer === undefined) {
      updateLifetime();
      timer = window.setInterval(updateLifetime, 1000);
    }
  };
  syncVisibleWork();
  const load = async () => {
    try {
      const response = await request('notes', {signal: controller.signal});
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const notes = await response.text();
      if (disposed) return;
      input.value = notes;
      input.disabled = false;
      save.disabled = false;
      status.textContent = 'Notes loaded.';
    } catch {
      if (!controller.signal.aborted) status.textContent = 'Could not load notes. Reload the workspace to retry.';
    }
  };

  const onSave = async () => {
    const notes = input.value;
    save.disabled = true;
    status.textContent = 'Saving notes…';
    try {
      const response = await request('notes', {
        method: 'PUT',
        headers: {'Content-Type': 'text/plain; charset=utf-8'},
        body: notes,
        signal: controller.signal,
      });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      if (!disposed) status.textContent = input.value === notes ? 'Notes saved.' : 'Earlier changes saved. Save again for your latest edits.';
    } catch {
      if (!controller.signal.aborted) status.textContent = 'Could not save notes. Try again.';
    } finally {
      if (!disposed) save.disabled = false;
    }
  };

  const onInput = () => {
    status.textContent = 'Unsaved changes.';
  };
  input.addEventListener('input', onInput);
  save.addEventListener('click', onSave);
  field.append(label, input);
  const streamStatus = document.createElement('p');
  streamStatus.dataset.streamConnection = '';
  streamStatus.textContent = 'Stream: unavailable';
  const pingButton = document.createElement('button');
  pingButton.type = 'button';
  pingButton.className = 'ui button';
  pingButton.textContent = 'Ping stream';
  pingButton.disabled = true;
  const reply = document.createElement('p');
  reply.dataset.streamReply = '';
  root.append(field, save, status, lifetime, streamStatus, pingButton, reply);
  load();

  /** @type {(() => void) | undefined} */
  let closeStream;
  /** @type {WebSocket | undefined} */
  let socket;
  let pings = 0;
  /** @param {MessageEvent} event */
  const onMessage = (event) => {
    if (disposed) return;
    let payload = null;
    try {
      payload = JSON.parse(event.data);
    } catch {
      return;
    }
    if (payload && typeof payload.hello === 'string' && !streamStatus.dataset.streamConnection) {
      streamStatus.dataset.streamConnection = payload.hello;
      streamStatus.textContent = `Stream: connected (${payload.hello})`;
      pingButton.disabled = false;
      return;
    }
    if (payload && payload.ping !== undefined) {
      reply.dataset.streamReply = String(payload.ping);
      reply.textContent = `Reply: ${payload.ping}`;
    }
  };
  const onClose = () => {
    pingButton.disabled = true;
    if (!disposed) streamStatus.textContent = 'Stream: closed';
  };
  const onPing = () => {
    if (socket?.readyState !== WebSocket.OPEN) return;
    pings += 1;
    socket.send(JSON.stringify({ping: `ping-${pings}`}));
  };
  try {
    const held = stream('stream');
    socket = held.socket;
    closeStream = held.close;
    if (!socket || socket.readyState === WebSocket.CLOSED || socket.readyState === WebSocket.CLOSING) {
      throw new Error('Stream unavailable');
    }
    socket.addEventListener('message', onMessage);
    socket.addEventListener('close', onClose);
    pingButton.addEventListener('click', onPing);
  } catch {
    streamStatus.textContent = 'Stream: unavailable';
  }

  const dispose = () => {
    if (disposed) return;
    disposed = true;
    controller.abort();
    syncVisibleWork();
    closeStream?.();
    socket?.removeEventListener('message', onMessage);
    socket?.removeEventListener('close', onClose);
    pingButton.removeEventListener('click', onPing);
    input.removeEventListener('input', onInput);
    save.removeEventListener('click', onSave);
    input.value = '';
    reply.textContent = '';
    delete streamStatus.dataset.streamConnection;
  };
  return {
    update: (presentation) => {
      visible = presentation.visible;
      lifetime.title = `${presentation.mode}: ${presentation.route}`;
      syncVisibleWork();
    },
    suspend: () => {
      suspended = true;
      syncVisibleWork();
    },
    resume: () => {
      suspended = false;
      syncVisibleWork();
    },
    retire: dispose,
    dispose,
  };
}
