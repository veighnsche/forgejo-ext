import {vi} from 'vitest';
import {initCommonOrganization, retireCommonOrganization} from './common-organization.js';
import {showModal} from '../modules/modal.ts';

vi.mock('../modules/modal.ts', () => ({showModal: vi.fn()}));

afterEach(() => {
  retireCommonOrganization();
  document.body.innerHTML = '';
});

test('org name input toggles the rename prompt only while initialized', () => {
  document.body.innerHTML = `<div class="organization settings options">
      <input id="org_name" data-org-name="oldname" value="oldname">
      <div id="org-name-change-prompt" class="tw-hidden"></div>
    </div>`;
  const input = document.getElementById('org_name');
  const prompt = document.getElementById('org-name-change-prompt');
  initCommonOrganization();
  initCommonOrganization();
  input.value = 'newname';
  input.dispatchEvent(new Event('input'));
  expect(prompt.classList.contains('tw-hidden')).toBe(false);
  input.value = 'oldname';
  input.dispatchEvent(new Event('input'));
  expect(prompt.classList.contains('tw-hidden')).toBe(true);

  retireCommonOrganization();
  input.value = 'othername';
  input.dispatchEvent(new Event('input'));
  expect(prompt.classList.contains('tw-hidden')).toBe(true);
  initCommonOrganization();
  input.dispatchEvent(new Event('input'));
  expect(prompt.classList.contains('tw-hidden')).toBe(false);
});

test('organization label actions bind and retire with the org feature', () => {
  document.body.innerHTML = `<div class="organization settings labels">
      <a class="new-label button">New Label</a>
      <form class="new-label form">
        <input class="label-name-input">
        <span class="label-exclusive-input-field"></span>
        <input class="label-exclusive-input" type="checkbox">
        <span class="label-exclusive-warning tw-hidden"></span>
      </form>
    </div>`;
  initCommonOrganization();
  initCommonOrganization();
  document.querySelector('.new-label.button').click();
  expect(showModal).toHaveBeenCalledTimes(1);
  expect(showModal).toHaveBeenCalledWith('new-label-modal', expect.any(Function));
  retireCommonOrganization();
  document.querySelector('.new-label.button').click();
  expect(showModal).toHaveBeenCalledTimes(1);
});

test('init outside organization pages and retire without init are no-ops', () => {
  document.body.innerHTML = '<div>plain</div>';
  expect(() => initCommonOrganization()).not.toThrow();
  expect(() => retireCommonOrganization()).not.toThrow();
});
