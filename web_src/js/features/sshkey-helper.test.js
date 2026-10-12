import {initSshKeyFormParser, retireSshKeyFormParser} from './sshkey-helper.js';

function installForm() {
  document.body.innerHTML = `<input id="ssh-key-content"><input id="ssh-key-title">`;
  return {
    content: document.getElementById('ssh-key-content'),
    title: document.getElementById('ssh-key-title'),
  };
}

afterEach(() => {
  retireSshKeyFormParser();
  document.body.innerHTML = '';
});

test('pasting a full key fills an empty title from the comment', () => {
  const {content, title} = installForm();
  initSshKeyFormParser();
  initSshKeyFormParser();
  content.value = 'ssh-ed25519 AAAAC3 comment@example';
  content.dispatchEvent(new Event('input'));
  expect(title.value).toBe('comment@example');
});

test('an existing title and partial keys are left untouched', () => {
  const {content, title} = installForm();
  initSshKeyFormParser();
  title.value = 'mine';
  content.value = 'ssh-ed25519 AAAAC3 other@example';
  content.dispatchEvent(new Event('input'));
  expect(title.value).toBe('mine');
  title.value = '';
  content.value = 'ssh-ed25519 AAAAC3';
  content.dispatchEvent(new Event('input'));
  expect(title.value).toBe('');
});

test('retire removes the listener and re-init binds it again', () => {
  const {content, title} = installForm();
  initSshKeyFormParser();
  retireSshKeyFormParser();
  content.value = 'ssh-ed25519 AAAAC3 comment@example';
  content.dispatchEvent(new Event('input'));
  expect(title.value).toBe('');
  initSshKeyFormParser();
  content.dispatchEvent(new Event('input'));
  expect(title.value).toBe('comment@example');
});

test('init without the form and retire without init are no-ops', () => {
  document.body.innerHTML = '';
  expect(() => initSshKeyFormParser()).not.toThrow();
  expect(() => retireSshKeyFormParser()).not.toThrow();
});
