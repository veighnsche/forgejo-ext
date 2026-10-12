export function mount(root, {request, stream}) {
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
  const update = () => {
    lifetime.textContent = `Mounted for ${Math.floor((Date.now() - started) / 1000)} seconds`;
  };
  update();
  const timer = window.setInterval(update, 1000);
  const controller = new AbortController();
  let disposed = false;
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

  let closeStream;
  let pings = 0;
  try {
    if (typeof stream !== 'function') throw new Error('Stream helper unavailable');
    const held = stream('stream');
    const socket = held.socket;
    closeStream = held.close;
    if (!socket || socket.readyState === WebSocket.CLOSED || socket.readyState === WebSocket.CLOSING) {
      throw new Error('Stream unavailable');
    }
    socket.addEventListener('message', (event) => {
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
    });
    socket.addEventListener('close', () => {
      pingButton.disabled = true;
      if (!disposed) streamStatus.textContent = 'Stream: closed';
    });
    pingButton.addEventListener('click', () => {
      if (socket.readyState !== WebSocket.OPEN) return;
      pings += 1;
      socket.send(JSON.stringify({ping: `ping-${pings}`}));
    });
  } catch {
    streamStatus.textContent = 'Stream: unavailable';
  }

  return () => {
    disposed = true;
    controller.abort();
    window.clearInterval(timer);
    closeStream?.();
    input.removeEventListener('input', onInput);
    save.removeEventListener('click', onSave);
  };
}
