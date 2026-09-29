import {vi} from 'vitest';
import {classifyWorkspaceNavigation, workspacePath, mountExtension, initExtensionPages, initExtensionWorkspace} from './extensions.js';

const origin = 'https://forge.example';
const base = `${origin}/-/extensions/workspace`;

test('admits native same-origin paths and preserves deep links', () => {
  expect(classifyWorkspaceNavigation('/team/repo/issues/7?sort=oldest#comment-1', base, origin)).toEqual({
    kind: 'frame',
    url: `${origin}/team/repo/issues/7?sort=oldest#comment-1`,
    path: '/team/repo/issues/7?sort=oldest#comment-1',
  });
  expect(workspacePath('/team/repo/issues/7', base, origin)).toBe('/team/repo/issues/7');
});

test('keeps auth and external destinations out of the frame', () => {
  expect(classifyWorkspaceNavigation('/user/login?redirect_to=%2Fteam', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/user/logout', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/user/activate?code=abc', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/user/two_factor', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/user/webauthn', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/user/recover_account', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/user/settings/security/two_factor/enroll', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/user/settings/applications', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/oauth2/authorize', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/login/openid', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/-/extensions/workspace?path=%2Fteam', base, origin).kind).toBe('reject');
  expect(classifyWorkspaceNavigation('/repo?access_token=secret', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/repo?monkey=usual', base, origin).kind).toBe('frame');
  expect(classifyWorkspaceNavigation('https://elsewhere.example/', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('data:text/html,unsafe', base, origin).kind).toBe('reject');
  expect(workspacePath('//elsewhere.example/', base, origin)).toBeNull();
  expect(workspacePath('/team%2frepo', base, origin)).toBeNull();
  expect(workspacePath('/team%5crepo', base, origin)).toBeNull();
  expect(workspacePath('/elsewhere', `${origin}/forge/-/extensions/workspace`, origin, '/forge')).toBeNull();
  expect(workspacePath('/forge/team', `${origin}/forge/-/extensions/workspace`, origin, '/forge')).toBe('/forge/team');
});

test('mounts an extension with explicit context and disposes its lifecycle', async () => {
  const root = document.createElement('div');
  root.dataset.extensionEntry = '/-/extensions/assets/example/main.js';
  root.dataset.extensionAssetBase = '/-/extensions/assets/example/';
  root.dataset.extensionApiBase = '/-/extensions/pages/example/home/api/';
  root.dataset.extensionId = 'example';
  root.dataset.extensionPageId = 'home';
  root.dataset.extensionSessionGeneration = 'session-a';
  const dispose = vi.fn();
  const mount = vi.fn(() => dispose);
  const importer = vi.fn(async () => ({mount}));
  const unmount = await mountExtension(root, importer);
  expect(importer).toHaveBeenCalledOnce();
  expect(mount).toHaveBeenCalledWith(root, expect.objectContaining({
    extensionId: 'example', pageId: 'home', panelId: undefined,
    apiBase: '/-/extensions/pages/example/home/api/', assetBase: '/-/extensions/assets/example/',
    sessionGeneration: 'session-a', request: expect.any(Function),
  }));
  const oldFetch = window.fetch;
  window.fetch = vi.fn().mockResolvedValue({status: 200});
  try {
    await mount.mock.calls[0][1].request('context', {headers: {'Content-Type': 'application/json'}});
    expect(window.fetch).toHaveBeenCalledWith(`${window.location.origin}/-/extensions/pages/example/home/api/context`, expect.objectContaining({
      credentials: 'same-origin', redirect: 'error', cache: 'no-store',
      headers: expect.any(Headers),
    }));
    expect(window.fetch.mock.calls[0][1].headers.get('X-Extension-Session-Generation')).toBe('session-a');
    expect(window.fetch.mock.calls[0][1].headers.get('Content-Type')).toBe('application/json');
    expect(() => mount.mock.calls[0][1].request('../other')).toThrow('Extension API URL is invalid');
  } finally {
    window.fetch = oldFetch;
  }
  unmount();
  expect(dispose).toHaveBeenCalledOnce();
});

test('keeps a mounted panel alive when its page enters BFCache', async () => {
  document.body.innerHTML = '<div data-extension-panel data-extension-id="example" data-extension-panel-id="notes" data-extension-entry="/-/extensions/assets/example/panel.js" data-extension-asset-base="/-/extensions/assets/example/" data-extension-api-base="/-/extensions/panels/example/notes/api/" data-extension-session-generation="session-a"></div>';
  const dispose = vi.fn();
  initExtensionPages(async () => ({mount: () => dispose}));
  await new Promise((resolve) => setTimeout(resolve, 0));
  const cached = new Event('pagehide');
  Object.defineProperty(cached, 'persisted', {value: true});
  window.dispatchEvent(cached);
  expect(dispose).not.toHaveBeenCalled();
  const final = new Event('pagehide');
  Object.defineProperty(final, 'persisted', {value: false});
  window.dispatchEvent(final);
  expect(dispose).toHaveBeenCalledOnce();
});

test('removes a cached extension page when the native session changes', async () => {
  document.body.innerHTML = '<div data-extension-page data-extension-id="example" data-extension-page-id="home" data-extension-entry="/-/extensions/assets/example/page.js" data-extension-asset-base="/-/extensions/assets/example/" data-extension-api-base="/-/extensions/pages/example/home/api/" data-extension-session-generation="session-a">private page</div>';
  const root = document.querySelector('[data-extension-page]');
  const dispose = vi.fn();
  const fetcher = vi.fn().mockResolvedValue({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-b'})});
  initExtensionPages(async () => ({mount: () => dispose}), fetcher);
  await new Promise((resolve) => setTimeout(resolve, 0));
  const cached = new Event('pagehide');
  Object.defineProperty(cached, 'persisted', {value: true});
  window.dispatchEvent(cached);
  expect(root.style.display).toBe('none');
  const restored = new Event('pageshow');
  Object.defineProperty(restored, 'persisted', {value: true});
  window.dispatchEvent(restored);
  await vi.waitFor(() => expect(root.textContent).toContain('Your session changed'));
  expect(root.textContent).not.toContain('private page');
  expect(dispose).toHaveBeenCalledOnce();
  expect(fetcher).toHaveBeenCalledWith(`${window.location.origin}/-/extensions/workspace?session_check=1`, {
    credentials: 'same-origin', cache: 'no-store', redirect: 'manual',
  });
  window.dispatchEvent(new Event('pagehide'));
});

test('workspace keeps panel roots and mirrors native frame navigation in its address', () => {
  document.body.innerHTML = '<main data-extension-workspace data-workspace-path="/team/repo" data-workspace-session-generation="session-a"><iframe data-extension-workspace-frame></iframe><span data-extension-workspace-location></span><div data-extension-panel></div></main>';
  window.history.replaceState(null, '', '/-/extensions/workspace');
  const frame = document.querySelector('iframe');
  let frameSource = '';
  let childURL = `${window.location.origin}/team/repo`;
  const childDocument = document.implementation.createHTMLDocument('frame');
  Object.defineProperty(frame, 'src', {
    get: () => frameSource,
    set: (value) => { frameSource = new URL(value, window.location.href).href },
  });
  const childWindow = new EventTarget();
  Object.defineProperty(childWindow, 'location', {get: () => ({href: childURL})});
  Object.defineProperty(frame, 'contentWindow', {get: () => childWindow});
  Object.defineProperty(frame, 'contentDocument', {get: () => childDocument});
  const pushState = vi.spyOn(window.history, 'pushState');
  initExtensionWorkspace();
  expect(new URL(window.location.href).searchParams.get('path')).toBe('/team/repo');
  const panel = document.querySelector('[data-extension-panel]');
  childURL = `${window.location.origin}/team/repo/issues`;
  frame.dispatchEvent(new Event('load'));
  expect(new URL(window.location.href).searchParams.get('path')).toBe('/team/repo/issues');
  expect(pushState).not.toHaveBeenCalled();
  expect(document.querySelector('[data-extension-panel]')).toBe(panel);
  window.history.replaceState({extensionWorkspacePath: '/team/repo'}, '', '/-/extensions/workspace?path=%2Fteam%2Frepo');
  window.dispatchEvent(new PopStateEvent('popstate'));
  expect(frameSource).toBe(`${window.location.origin}/team/repo`);
  pushState.mockRestore();
  window.dispatchEvent(new Event('pagehide'));
});

test('workspace leaves modified links and downloads to the frame and routes excluded forms at top level', () => {
  document.body.innerHTML = '<main data-extension-workspace data-workspace-path="/team" data-workspace-session-generation="session-a"><iframe data-extension-workspace-frame></iframe><span data-extension-workspace-location></span></main>';
  window.history.replaceState(null, '', '/-/extensions/workspace');
  const frame = document.querySelector('iframe');
  Object.defineProperty(frame, 'src', {get: () => '', set: () => {}});
  const childDocument = document.implementation.createHTMLDocument('frame');
  const childWindow = new EventTarget();
  Object.defineProperty(childWindow, 'location', {value: {href: `${window.location.origin}/team`}});
  Object.defineProperty(frame, 'contentWindow', {value: childWindow});
  Object.defineProperty(frame, 'contentDocument', {value: childDocument});
  initExtensionWorkspace();
  frame.dispatchEvent(new Event('load'));

  const blocked = childDocument.createElement('a');
  blocked.href = '/-/extensions/workspace';
  childDocument.body.append(blocked);
  const modified = new MouseEvent('click', {bubbles: true, cancelable: true, ctrlKey: true});
  blocked.dispatchEvent(modified);
  expect(modified.defaultPrevented).toBe(false);
  blocked.download = 'file';
  const download = new MouseEvent('click', {bubbles: true, cancelable: true});
  blocked.dispatchEvent(download);
  expect(download.defaultPrevented).toBe(false);
  blocked.removeAttribute('download');
  const plain = new MouseEvent('click', {bubbles: true, cancelable: true});
  blocked.dispatchEvent(plain);
  expect(plain.defaultPrevented).toBe(true);

  const form = childDocument.createElement('form');
  form.action = '/user/settings/security/two_factor/enroll';
  childDocument.body.append(form);
  form.dispatchEvent(new Event('submit', {bubbles: true, cancelable: true}));
  expect(form.target).toBe('_top');
  window.dispatchEvent(new Event('pagehide'));
});

test('hides a restored workspace until the live native generation matches and clears it on account change', async () => {
  document.body.innerHTML = '<main data-extension-workspace data-workspace-path="/team" data-workspace-session-generation="session-a"><iframe data-extension-workspace-frame></iframe><span data-extension-workspace-location></span><div data-extension-panel data-extension-id="example" data-extension-panel-id="notes" data-extension-entry="/-/extensions/assets/example/panel.js" data-extension-asset-base="/-/extensions/assets/example/" data-extension-api-base="/-/extensions/panels/example/notes/api/" data-extension-session-generation="session-a"></div></main>';
  window.history.replaceState(null, '', '/-/extensions/workspace');
  const workspace = document.querySelector('[data-extension-workspace]');
  const frame = workspace.querySelector('iframe');
  let frameSource;
  Object.defineProperty(frame, 'src', {get: () => frameSource, set: (value) => { frameSource = value }});
  const dispose = vi.fn();
  initExtensionPages(async () => ({mount: () => dispose}));
  await new Promise((resolve) => setTimeout(resolve, 0));
  const fetcher = vi.fn().mockResolvedValueOnce({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-a'})})
    .mockResolvedValueOnce({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-b'})});
  initExtensionWorkspace(fetcher);
  const cached = new Event('pagehide');
  Object.defineProperty(cached, 'persisted', {value: true});
  window.dispatchEvent(cached);
  expect(workspace.style.display).toBe('none');
  const restored = new Event('pageshow');
  Object.defineProperty(restored, 'persisted', {value: true});
  window.dispatchEvent(restored);
  expect(workspace.style.display).toBe('none');
  await vi.waitFor(() => expect(workspace.style.display).toBe(''));
  expect(dispose).not.toHaveBeenCalled();
  expect(fetcher).toHaveBeenCalledWith(`${window.location.origin}/-/extensions/workspace?session_check=1`, {
    credentials: 'same-origin', cache: 'no-store', redirect: 'manual',
  });
  window.dispatchEvent(new Event('focus'));
  expect(workspace.style.display).toBe('none');
  await vi.waitFor(() => expect(workspace.textContent).toContain('Your session changed'));
  expect(frameSource).toBe('about:blank');
  expect(workspace.querySelector('[data-extension-panel]')).toBeNull();
  expect(dispose).toHaveBeenCalledOnce();
  window.dispatchEvent(new Event('pagehide'));
});

test('ignores a session check completed after the workspace was cached', async () => {
  document.body.innerHTML = '<main data-extension-workspace data-workspace-path="/team" data-workspace-session-generation="session-a"><iframe data-extension-workspace-frame></iframe><span data-extension-workspace-location></span></main>';
  window.history.replaceState(null, '', '/-/extensions/workspace');
  const workspace = document.querySelector('[data-extension-workspace]');
  const frame = workspace.querySelector('iframe');
  Object.defineProperty(frame, 'src', {get: () => '', set: () => {}});
  let resolveOld;
  let resolveCurrent;
  const fetcher = vi.fn().mockImplementationOnce(() => new Promise((resolve) => { resolveOld = resolve }))
    .mockImplementationOnce(() => new Promise((resolve) => { resolveCurrent = resolve }));
  initExtensionWorkspace(fetcher);
  window.dispatchEvent(new Event('focus'));
  await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
  const cached = new Event('pagehide');
  Object.defineProperty(cached, 'persisted', {value: true});
  window.dispatchEvent(cached);
  const restored = new Event('pageshow');
  Object.defineProperty(restored, 'persisted', {value: true});
  window.dispatchEvent(restored);
  await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
  resolveOld({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-b'})});
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(workspace.style.display).toBe('none');
  expect(workspace.querySelector('iframe')).toBe(frame);
  resolveCurrent({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-a'})});
  await vi.waitFor(() => expect(workspace.style.display).toBe(''));
  window.dispatchEvent(new Event('pagehide'));
});
