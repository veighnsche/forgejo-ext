import {vi} from 'vitest';
import {initModalClose, showModal} from './modal.ts';

afterEach(() => {
  document.body.innerHTML = '';
});

test('re-showing a modal replaces its approve and cancel handlers instead of stacking', () => {
  document.body.innerHTML = '<dialog id="m"><button class="cancel">cancel</button><button class="ok">ok</button></dialog>';
  const modal = document.getElementById('m');
  modal.showModal = vi.fn();
  modal.close = vi.fn();
  const first = vi.fn();
  const second = vi.fn();
  showModal('m', first);
  showModal('m', second);
  modal.querySelector('.ok').click();
  expect(first).not.toHaveBeenCalled();
  expect(second).toHaveBeenCalledTimes(1);
  modal.querySelector('.cancel').click();
  expect(modal.close).toHaveBeenCalledTimes(1);
});

test('initModalClose binds one backdrop handler after repeated init', () => {
  initModalClose();
  initModalClose();
  document.body.innerHTML = '<dialog id="d" open></dialog>';
  const dialog = document.getElementById('d');
  dialog.close = vi.fn();
  dialog.dispatchEvent(new MouseEvent('click', {bubbles: true}));
  expect(dialog.close).toHaveBeenCalledTimes(1);
});
