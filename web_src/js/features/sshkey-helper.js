// Tracks the attached input so retire removes exactly what init added.
const attached = new Set();

function onSshKeyInput(event) {
  // Parse SSH Key
  const arrays = event.currentTarget.value.split(' ');
  const title = document.getElementById('ssh-key-title');
  if (!title.value && arrays.length === 3 && arrays[2] !== '') {
    title.value = arrays[2];
  }
}

export function initSshKeyFormParser() {
  const content = document.getElementById('ssh-key-content');
  if (!content || attached.has(content)) return;
  content.addEventListener('input', onSshKeyInput);
  attached.add(content);
}

export function retireSshKeyFormParser() {
  for (const el of attached) {
    el.removeEventListener('input', onSshKeyInput);
    attached.delete(el);
  }
}
