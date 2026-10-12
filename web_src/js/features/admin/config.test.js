import {vi} from 'vitest';
import {initAdminConfigs, retireAdminConfigs} from './config.js';
import {POST} from '../../modules/fetch.js';
import {showTemporaryTooltip} from '../../modules/tippy.js';

vi.mock('../../modules/fetch.js', () => ({POST: vi.fn()}));
vi.mock('../../modules/tippy.js', () => ({showTemporaryTooltip: vi.fn()}));

function installConfig() {
  document.body.innerHTML = `<div class="page-content admin config">
    <input type="checkbox" data-config-dyn-key="some.key">
  </div>`;
  return document.querySelector('input[data-config-dyn-key]');
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

afterEach(() => {
  retireAdminConfigs();
  document.body.innerHTML = '';
});

test('change posts the key/value pair once even after repeated init', async () => {
  POST.mockResolvedValue({json: async () => ({})});
  const box = installConfig();
  initAdminConfigs();
  initAdminConfigs();
  box.checked = true;
  box.dispatchEvent(new Event('change'));
  await flush();
  expect(POST).toHaveBeenCalledTimes(1);
  const [url, opts] = POST.mock.calls[0];
  expect(url).toBe('/admin/config');
  expect(opts.data.get('key')).toBe('some.key');
  expect(opts.data.get('value')).toBe('true');
});

test('a failed update shows a tooltip and reverts the checkbox', async () => {
  POST.mockResolvedValue({json: async () => ({errorMessage: 'nope'})});
  const box = installConfig();
  initAdminConfigs();
  box.checked = true;
  box.dispatchEvent(new Event('change'));
  await flush();
  expect(showTemporaryTooltip).toHaveBeenCalledTimes(1);
  expect(box.checked).toBe(false);
});

test('retire removes the listener and re-init binds it again', async () => {
  POST.mockResolvedValue({json: async () => ({})});
  const box = installConfig();
  initAdminConfigs();
  retireAdminConfigs();
  box.dispatchEvent(new Event('change'));
  await flush();
  expect(POST).not.toHaveBeenCalled();
  initAdminConfigs();
  box.dispatchEvent(new Event('change'));
  await flush();
  expect(POST).toHaveBeenCalledTimes(1);
});

test('init without the config page and retire without init are no-ops', () => {
  document.body.innerHTML = '';
  expect(() => initAdminConfigs()).not.toThrow();
  expect(() => retireAdminConfigs()).not.toThrow();
});
