import {vi} from 'vitest';
import {initPageFeatures, registerPageFeature, retirePageFeatures, setPageConfig} from '../native-lifecycle.js';
import {initColorPickers, retireColorPickers} from './colorpicker.js';

vi.mock('../modules/tippy.js', () => ({createTippy: vi.fn(() => ({destroy: vi.fn()}))}));

function installPicker() {
  document.body.innerHTML = `<div class="page-content"><div class="js-color-picker-input"><input value="#ff0000"><div class="precolors"><span class="color" data-color-hex="#00ff00"></span></div></div></div>`;
}

afterEach(() => {
  retireColorPickers();
  try { retirePageFeatures() } catch {}
  document.body.innerHTML = '';
  vi.clearAllMocks();
});

test('color pickers mount once per entry and retire destroys', async () => {
  const {createTippy} = await import('../modules/tippy.js');
  installPicker();
  await initColorPickers();
  await initColorPickers();
  expect(createTippy).toHaveBeenCalledTimes(1);
  expect(document.querySelector('.preview-square')).not.toBeNull();
  const input = document.querySelector('.js-color-picker-input input');
  input.value = '#00ff00';
  input.dispatchEvent(new Event('input'));
  expect(document.querySelector('.preview-square').style.color).toBe('#00ff00');
  retireColorPickers();
  expect(createTippy.mock.results[0].value.destroy).toHaveBeenCalledTimes(1);
  expect(document.querySelector('.preview-square')).toBeNull();
  // re-entry mounts fresh
  installPicker();
  createTippy.mockClear();
  await initColorPickers();
  expect(createTippy).toHaveBeenCalledTimes(1);
  expect(document.querySelector('.preview-square')).not.toBeNull();
});

test('contribution-owned pickers are never mounted', async () => {
  const {createTippy} = await import('../modules/tippy.js');
  document.body.innerHTML = `<div data-extension-panel><div class="js-color-picker-input"><input value="#ff0000"></div></div>`;
  await initColorPickers();
  expect(createTippy).not.toHaveBeenCalled();
  expect(document.querySelector('.preview-square')).toBeNull();
});

test('page seam drives color-picker init and retire', async () => {
  const {createTippy} = await import('../modules/tippy.js');
  installPicker();
  registerPageFeature('test-b03-color', {init: () => initColorPickers(), retire: retireColorPickers});
  setPageConfig({});
  initPageFeatures();
  await vi.waitFor(() => expect(createTippy).toHaveBeenCalledTimes(1));
  retirePageFeatures();
  expect(document.querySelector('.preview-square')).toBeNull();
});
