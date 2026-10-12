import {vi} from 'vitest';
import {initAdminEmails, retireAdminEmails} from './emails.js';
import {showModal} from '../../modules/modal.ts';

vi.mock('../../modules/modal.ts', () => ({showModal: vi.fn()}));

function installEmails() {
  document.body.innerHTML = `
    <input type="hidden" id="form-uid"><input type="hidden" id="form-email">
    <input type="hidden" id="form-primary"><input type="hidden" id="form-activate">
    <a class="link-email-action" data-uid="7" data-email="a@example.com" data-primary="true" data-activate="false">change</a>`;
  return document.querySelector('.link-email-action');
}

afterEach(() => {
  retireAdminEmails();
  document.body.innerHTML = '';
});

test('click fills the change-email form and opens the modal once', () => {
  const link = installEmails();
  initAdminEmails();
  initAdminEmails();
  link.click();
  expect(document.getElementById('form-uid').value).toBe('7');
  expect(document.getElementById('form-email').value).toBe('a@example.com');
  expect(document.getElementById('form-primary').value).toBe('true');
  expect(document.getElementById('form-activate').value).toBe('false');
  expect(showModal).toHaveBeenCalledTimes(1);
  expect(showModal).toHaveBeenCalledWith('change-email-modal', undefined);
});

test('retire removes the handler and re-init binds it again', () => {
  const link = installEmails();
  initAdminEmails();
  retireAdminEmails();
  link.click();
  expect(showModal).not.toHaveBeenCalled();
  initAdminEmails();
  link.click();
  expect(showModal).toHaveBeenCalledTimes(1);
});

test('init without actions and retire without init are no-ops', () => {
  document.body.innerHTML = '';
  expect(() => initAdminEmails()).not.toThrow();
  expect(() => retireAdminEmails()).not.toThrow();
});
