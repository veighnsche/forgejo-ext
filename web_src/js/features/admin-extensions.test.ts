import {afterEach, beforeEach, expect, test, vi} from 'vitest';
import {initAdminExtensions, retireAdminExtensions} from './admin-extensions.ts';

let reload: ReturnType<typeof vi.fn>;
let get: ReturnType<typeof vi.fn>;
let hidden = false;
function row(state = 'activating', generation = '1') {
  document.body.innerHTML = `<table id="ext-status-table" data-status-url="/admin/extensions/status"><tbody><tr id="ext-row-sample" data-ext-state="${state}" data-ext-generation="${generation}" data-ext-installed="true"><td class="ext-progress"></td></tr></tbody></table>`;
}
function status(state = 'activating', generation = '1') {
  return [{id: 'sample', state, generation, installed: true, progress: 'Waiting'}];
}
beforeEach(() => {
  vi.useFakeTimers();
  hidden = false;
  vi.spyOn(document, 'hidden', 'get').mockImplementation(() => hidden);
  reload = vi.fn();
  vi.stubGlobal('location', {...window.location, reload});
  get = vi.fn().mockResolvedValue({ok: true, json: async () => status()});
  vi.stubGlobal('fetch', get);
  row();
});
afterEach(() => {
  retireAdminExtensions();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  document.body.innerHTML = '';
});

test('terminal failures do not poll, including unconfirmed failures', async () => {
  row('failed');
  document.querySelector('tr')!.setAttribute('data-ext-unconfirmed', 'true');
  initAdminExtensions();
  await vi.advanceTimersByTimeAsync(6000);
  expect(get).not.toHaveBeenCalled();
});

test('progress reads are GET only and preserve escaped text', async () => {
  get.mockResolvedValue({ok: true, json: async () => [{...status()[0], progress: '<b>Waiting</b>'}]});
  initAdminExtensions();
  await vi.advanceTimersByTimeAsync(2000);
  expect(get).toHaveBeenCalledTimes(1);
  expect(get.mock.calls[0][1].method).toBe('GET');
  expect(document.querySelector('.ext-progress')!.textContent).toBe('<b>Waiting</b>');
  expect(document.querySelector('.ext-progress b')).toBeNull();
  expect(reload).not.toHaveBeenCalled();
});

for (const [state, generation] of [['enabled', '1'], ['activating', '2']]) {
  test(`reloads server actions and pins on ${state}/${generation}`, async () => {
    get.mockResolvedValue({ok: true, json: async () => status(state, generation)});
    initAdminExtensions();
    await vi.advanceTimersByTimeAsync(6000);
    expect(reload).toHaveBeenCalledTimes(1);
    expect(get).toHaveBeenCalledTimes(1);
  });
}

test('hidden page aborts pending observation and resumes when visible', async () => {
  get.mockImplementation(() => new Promise(() => {}));
  initAdminExtensions();
  await vi.advanceTimersByTimeAsync(2000);
  const signal = get.mock.calls[0][1].signal;
  hidden = true;
  document.dispatchEvent(new Event('visibilitychange'));
  expect(signal.aborted).toBe(true);
  await vi.advanceTimersByTimeAsync(6000);
  expect(get).toHaveBeenCalledTimes(1);
  hidden = false;
  document.dispatchEvent(new Event('visibilitychange'));
  expect(get).toHaveBeenCalledTimes(2);
  retireAdminExtensions();
  expect(get.mock.calls[1][1].signal.aborted).toBe(true);
});
