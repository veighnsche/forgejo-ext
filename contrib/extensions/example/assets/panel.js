export function mount(root, {request}) {
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
  root.append(field, save, status, lifetime);
  load();

  return () => {
    disposed = true;
    controller.abort();
    window.clearInterval(timer);
    input.removeEventListener('input', onInput);
    save.removeEventListener('click', onSave);
  };
}
