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
  const dispose = vi.fn();
  const mount = vi.fn(() => dispose);
  const importer = vi.fn(async () => ({mount}));
  const unmount = await mountExtension(root, importer);
  expect(importer).toHaveBeenCalledOnce();
  expect(mount).toHaveBeenCalledWith(root, {
    extensionId: 'example', pageId: 'home', panelId: undefined,
    apiBase: '/-/extensions/pages/example/home/api/', assetBase: '/-/extensions/assets/example/',
  });
  unmount();
  expect(dispose).toHaveBeenCalledOnce();
});

test('keeps a mounted panel alive when its page enters BFCache', async () => {
  document.body.innerHTML = '<div data-extension-panel data-extension-id="example" data-extension-panel-id="notes" data-extension-entry="/-/extensions/assets/example/panel.js" data-extension-asset-base="/-/extensions/assets/example/"></div>';
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

test('workspace replaces address path when frame navigates without adding outer history', () => {
  document.body.innerHTML = '<main data-extension-workspace data-workspace-path="/team/repo"><iframe data-extension-workspace-frame></iframe><span data-extension-workspace-location></span></main>';
  window.history.replaceState(null, '', '/-/extensions/workspace');
  const frame = document.querySelector('iframe');
  let frameSource = '';
  let childURL = `${window.location.origin}/team/repo`;
  const childDocument = document.implementation.createHTMLDocument('frame');
  Object.defineProperty(frame, 'src', {
    get: () => frameSource,
    set: (value) => { frameSource = new URL(value, window.location.href).href },
  });
  Object.defineProperty(frame, 'contentWindow', {get: () => ({location: {href: childURL}})});
  Object.defineProperty(frame, 'contentDocument', {get: () => childDocument});
  const pushState = vi.spyOn(window.history, 'pushState');
  initExtensionWorkspace();
  expect(new URL(window.location.href).searchParams.get('path')).toBe('/team/repo');
  childURL = `${window.location.origin}/team/repo/issues`;
  frame.dispatchEvent(new Event('load'));
  expect(new URL(window.location.href).searchParams.get('path')).toBe('/team/repo/issues');
  expect(pushState).not.toHaveBeenCalled();
  pushState.mockRestore();
});
