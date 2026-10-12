import {vi} from 'vitest';
import {initAdminCommon, retireAdminCommon} from './common.js';
import {showModal} from '../../modules/modal.ts';

vi.mock('../common-global.js', () => ({checkAppUrl: vi.fn()}));
vi.mock('../../modules/fetch.js', () => ({POST: vi.fn()}));
vi.mock('../../modules/modal.ts', () => ({showModal: vi.fn()}));

afterEach(() => {
  retireAdminCommon();
  document.body.innerHTML = '';
});

test('login type change toggles local fields and required attributes', () => {
  document.body.innerHTML = `<div class="page-content admin"><div class="admin new user">
    <select id="login_type" data-password="required">
      <option value="0-1" selected>Forgejo</option><option value="2-1">LDAP</option>
    </select>
    <input id="user_name"><input id="login_name"><input id="password" type="password">
    <div class="non-local"></div><div class="local tw-hidden"></div>
  </div></div>`;
  initAdminCommon();
  const loginType = document.getElementById('login_type');
  loginType.value = '2-1';
  loginType.dispatchEvent(new Event('change'));
  expect(document.querySelector('.local').classList.contains('tw-hidden')).toBe(true);
  expect(document.querySelector('.non-local').classList.contains('tw-hidden')).toBe(false);
  expect(document.getElementById('login_name').hasAttribute('required')).toBe(true);
  expect(document.getElementById('password').hasAttribute('required')).toBe(false);

  loginType.value = '0-1';
  loginType.dispatchEvent(new Event('change'));
  expect(document.querySelector('.local').classList.contains('tw-hidden')).toBe(false);
  expect(document.getElementById('login_name').hasAttribute('required')).toBe(false);
  expect(document.getElementById('password').hasAttribute('required')).toBe(true);
});

function installNotice() {
  document.body.innerHTML = `<div class="page-content admin"><div class="admin notice">
    <table class="select table"><tbody><tr>
      <td><div class="ui checkbox" data-id="11"><input type="checkbox"></div></td>
      <td><a class="view-detail">view</a><span class="notice-description">detail text</span></td>
    </tr></tbody></table>
    <a class="select action" data-action="select-all">all</a>
    <a class="select action" data-action="inverse">inverse</a>
    <div id="detail-modal"><div class="content"><pre></pre></div></div>
  </div></div>`;
  return document.querySelector('.ui.checkbox input');
}

test('notice select actions toggle each checkbox exactly once per click', () => {
  const checkbox = installNotice();
  initAdminCommon();
  initAdminCommon();
  document.querySelector('[data-action="select-all"]').click();
  expect(checkbox.checked).toBe(true);
  document.querySelector('[data-action="inverse"]').click();
  expect(checkbox.checked).toBe(false);
});

test('view detail fills the modal from its row and opens it once', () => {
  installNotice();
  initAdminCommon();
  initAdminCommon();
  document.querySelector('.view-detail').click();
  expect(document.querySelector('#detail-modal .content pre').textContent).toBe('detail text');
  expect(showModal).toHaveBeenCalledTimes(1);
  expect(showModal).toHaveBeenCalledWith('detail-modal', undefined);
});

test('retire removes notice handlers and re-init binds them again', () => {
  const checkbox = installNotice();
  initAdminCommon();
  retireAdminCommon();
  document.querySelector('[data-action="select-all"]').click();
  document.querySelector('.view-detail').click();
  expect(checkbox.checked).toBe(false);
  expect(showModal).not.toHaveBeenCalled();
  initAdminCommon();
  document.querySelector('[data-action="select-all"]').click();
  expect(checkbox.checked).toBe(true);
});

test('init outside admin pages and retire without init are no-ops', () => {
  document.body.innerHTML = '<div>plain</div>';
  expect(() => initAdminCommon()).not.toThrow();
  expect(() => retireAdminCommon()).not.toThrow();
});
