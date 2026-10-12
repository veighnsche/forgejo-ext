import {vi} from 'vitest';
import {readFileSync} from 'node:fs';
import {registerPageFeature, retirePageFeatures} from '../native-lifecycle.js';
import {classifyWorkspaceNavigation, workspacePath, workspaceDirectPath, initExtensionPages, initExtensionWorkspace} from './extensions.js';

const origin = 'https://forge.example';
const base = `${origin}/team/repo`;

test('repository product tabs precede native unit tabs in the overflow menu', () => {
  const template = readFileSync('templates/repo/header.tmpl', 'utf8');
  const productTabs = template.indexOf('{{with .ExtensionPages}}{{range index . "repository"}}');
  const codeTab = template.indexOf('{{if .Permission.CanRead $.UnitTypeCode}}', template.indexOf('<overflow-menu'));
  expect(productTabs).toBeGreaterThan(template.indexOf('<overflow-menu'));
  expect(productTabs).toBeLessThan(codeTab);
  expect(template.match(/range index . "repository"/g)).toHaveLength(1);
  expect(template.slice(productTabs, codeTab)).toContain('{{if ne .Permission "admin"}}');
});

test('admits native same-origin paths and preserves deep links', () => {
  expect(classifyWorkspaceNavigation('/team/repo/issues/7?sort=oldest#comment-1', base, origin)).toEqual({
    kind: 'native',
    url: `${origin}/team/repo/issues/7?sort=oldest#comment-1`,
    path: '/team/repo/issues/7?sort=oldest#comment-1',
  });
  expect(workspacePath('/team/repo/issues/7', base, origin)).toBe('/team/repo/issues/7');
  expect(workspacePath('/workspace', base, origin)).toBe('/workspace');
  expect(workspacePath('/team/repo/workspace', base, origin)).toBe('/team/repo/workspace');
  expect(workspacePath('/workspace/new', base, origin)).toBe('/workspace/new');
  expect(workspacePath('/workspace/requests/d0123456789abcdef01234567', base, origin)).toBe('/workspace/requests/d0123456789abcdef01234567');
});

test('classifies the workspace root and explicit aliases as direct', () => {
  for (const [pathname, sub] of [['/workspace', ''], ['/team/repo/workspace', ''], ['/forge/workspace', '/forge'],
    ['/workspace/new', ''], ['/forge/workspace/new', '/forge'],
    ['/workspace/requests/d0123456789abcdef01234567', ''], ['/forge/workspace/requests/d0123456789abcdef01234567', '/forge']]) {
    expect(workspaceDirectPath(pathname, sub)).toBe(true);
  }
  for (const [pathname, sub] of [['/workspace/requests', ''], ['/workspace/requests/', ''],
    ['/workspace/requests/bad', ''], ['/workspace/requests/d0123456789abcdef0123456', ''],
    ['/workspace/requests/d0123456789ABCDEF01234567', ''], ['/workspace/requests/d0123456789abcdef01234567/extra', ''],
    ['/workspace/new/extra', ''], ['/workspace/news', ''], ['/workspace/old', ''],
    ['/user2/repo1/workspace/new', ''], ['/user2/repo1/workspace/requests/d0123456789abcdef01234567', ''],
    ['/workspace/new', '/forge'], ['/other/workspace/new', '/forge'], ['/forgefoo/workspace/new', '/forge'], ['/issues', '']]) {
    expect(workspaceDirectPath(pathname, sub)).toBe(false);
  }
});

test('keeps auth and external destinations for top-level navigation', () => {
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
  expect(classifyWorkspaceNavigation('/-/native/session', base, origin).kind).toBe('reject');
  expect(classifyWorkspaceNavigation('/repo?access_token=secret', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/repo?monkey=usual', base, origin).kind).toBe('native');
  expect(classifyWorkspaceNavigation('https://elsewhere.example/', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('data:text/html,unsafe', base, origin).kind).toBe('reject');
  expect(workspacePath('//elsewhere.example/', base, origin)).toBeNull();
  expect(workspacePath('/team%2frepo', base, origin)).toBeNull();
  expect(workspacePath('/team%5crepo', base, origin)).toBeNull();
  expect(workspacePath('/elsewhere', `${origin}/forge/team/repo`, origin, '/forge')).toBeNull();
  expect(workspacePath('/forge/team', `${origin}/forge/team/repo`, origin, '/forge')).toBe('/forge/team');
});

// Mount validation, scoped transport and the mount/update/suspend/resume/retire
// lifecycle are owned by extension-mount.js and covered there; these tests
// cover host mounting, session checks and navigation behavior.
test('keeps a mounted panel alive when its page enters BFCache', async () => {
  document.body.innerHTML = '<div data-extension-panel data-app-sub-url="" data-extension-id="example" data-extension-panel-id="notes" data-extension-entry="/-/native/assets/example/panel.js" data-extension-asset-base="/-/native/assets/example/" data-extension-api-base="/-/native/panels/example/notes/api/" data-extension-session-generation="session-a"></div>';
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
  document.body.innerHTML = '<div data-extension-page data-app-sub-url="" data-extension-id="example" data-extension-page-id="home" data-extension-entry="/-/native/assets/example/page.js" data-extension-asset-base="/-/native/assets/example/" data-extension-api-base="/-/native/pages/example/home/api/" data-extension-session-generation="session-a">private page</div>';
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
  expect(fetcher).toHaveBeenCalledWith(`${window.location.origin}/-/native/session`, {
    credentials: 'same-origin', cache: 'no-store', redirect: 'manual',
  });
  window.dispatchEvent(new Event('pagehide'));
});

// NFX-A02 region_adopt: fixtures mirror the real wire format. Every eligible
// document is a whole native page (head title, body data-page-data config,
// navbar, .page-content region, footer) with the persistent panels home from
// the shared layout (templates/base/footer.tmpl + extensions/panels.tmpl).
function pageRootMarkup({pageId = 'home', generation = 'session-a', appSubURL = '', extra = ''} = {}) {
  return `<div data-extension-page data-app-sub-url="${appSubURL}" data-extension-id="example" data-extension-page-id="${pageId}" data-extension-entry="/-/native/assets/example/page.js" data-extension-asset-base="/-/native/assets/example/" data-extension-api-base="/-/native/pages/example/${pageId}/api/" data-extension-session-generation="${generation}"${extra}></div>`;
}

function panelRootMarkup({panelId = 'notes', generation = 'session-a', appSubURL = ''} = {}) {
  return `<div data-extension-panel data-app-sub-url="${appSubURL}" data-extension-id="example" data-extension-panel-id="${panelId}" data-extension-entry="/-/native/assets/example/panel.js" data-extension-asset-base="/-/native/assets/example/" data-extension-api-base="/-/native/panels/example/${panelId}/api/" data-extension-session-generation="${generation}"></div>`;
}

function homeMarkup({generation = 'session-a', appSubURL = '', hidden = false, panels = ''} = {}) {
  return `<aside id="extension-panels" data-extension-panels data-app-sub-url="${appSubURL}" data-extension-session-generation="${generation}"${hidden ? ' hidden inert' : ''}>${panels}</aside>`;
}

function nativeDocument({title, pageData, navbar = '', content, home}) {
  return `<html><head><title>${title}</title></head><body data-page-data='${JSON.stringify(pageData)}'><nav id="navbar">${navbar}</nav><div class="full height">${content}</div>${home}<footer></footer></body></html>`;
}

function directContent(slot, cls = 'page-content extensions') {
  return `<div role="main" aria-label="Workspace" class="${cls}"><div class="ui container">${slot}</div></div>`;
}

function plainAdminContent(slot) {
  return `<div role="main" class="page-content admin"><div class="ui container flex-container"><div class="admin-navbar">Admin</div><div class="flex-container-main">${slot}</div></div></div>`;
}

function liveMarkup({content, home, navbar = ''}) {
  return `<nav id="navbar">${navbar}</nav><div class="full height">${content}</div>${home}<footer></footer>`;
}

function clickWorkspaceLink(href) {
  const link = document.createElement('a');
  link.href = href;
  document.body.append(link);
  link.click();
  link.remove();
}

function navigationResponse(html, generation = 'session-a') {
  return new Response(html, {headers: {'X-Extension-Session-Generation': generation}});
}

test('transplant keeps one home, panels and address across contribution -> native', async () => {
  const savedConfig = window.config;
  window.history.replaceState({nativeState: 'retained'}, '', '/workspace');
  document.body.innerHTML = liveMarkup({
    content: directContent(`Global Workspace${pageRootMarkup()}`),
    home: homeMarkup({panels: '<div id="notes-owner">notes</div>'}),
    navbar: '<a class="item" href="/admin/users">Users</a><a class="item active" aria-current="page" href="/workspace">Workspace</a>',
  });
  window.config = {appSubUrl: '', pageData: {extension: {id: 'example'}}};
  const owner = document.getElementById('notes-owner');
  const home = document.querySelector('[data-extension-panels]');
  const adminPageData = {adminUserListSearchForm: {SortType: 'name'}};
  const workspacePageData = {extension: {id: 'example'}};
  const fetcher = vi.fn(async (url) => navigationResponse(url.includes('/admin/users') ?
    nativeDocument({title: 'Users - Forgejo', pageData: adminPageData, content: plainAdminContent('<form id="user-list-search-form"></form>'),
      navbar: '<a class="item" href="/admin/users">Users</a><a class="item" href="/workspace">Workspace</a>',
      home: homeMarkup({hidden: true})}) :
    nativeDocument({title: 'Workspace', pageData: workspacePageData, content: directContent(`Global Workspace${pageRootMarkup()}`),
      navbar: '<a class="item" href="/admin/users">Users</a><a class="item active" aria-current="page" href="/workspace">Workspace</a>',
      home: homeMarkup()})));
  const pushState = vi.spyOn(window.history, 'pushState');
  try {
    initExtensionWorkspace(fetcher);
    expect(home.hasAttribute('hidden')).toBe(false);
    clickWorkspaceLink('/admin/users?sort=oldest#list');
    await vi.waitFor(() => expect(window.location.pathname + window.location.search + window.location.hash).toBe('/admin/users?sort=oldest#list'));
    expect(document.title).toBe('Users - Forgejo');
    expect(document.querySelector('#user-list-search-form')).not.toBeNull();
    expect(window.config.pageData).toEqual(adminPageData);
    expect(document.querySelector('[data-extension-panels]')).toBe(home);
    expect(home.hasAttribute('hidden')).toBe(true);
    expect(home.hasAttribute('inert')).toBe(true);
    expect(document.getElementById('notes-owner')).toBe(owner);
    expect(owner.isConnected).toBe(true);
    expect(pushState).toHaveBeenCalledOnce();
    expect(document.querySelector('#navbar a[href="/workspace"]').classList.contains('active')).toBe(false);
    expect(document.querySelector('#navbar a[href="/workspace"]').hasAttribute('aria-current')).toBe(false);
    window.history.replaceState({nativeState: 'retained'}, '', '/workspace');
    window.dispatchEvent(new PopStateEvent('popstate'));
    await vi.waitFor(() => expect(document.querySelector('.page-content').textContent).toContain('Global Workspace'));
    expect(home.hasAttribute('hidden')).toBe(false);
    expect(home.hasAttribute('inert')).toBe(false);
    expect(window.config.pageData).toEqual(workspacePageData);
    expect(window.history.state.nativeState).toBe('retained');
    expect(document.getElementById('notes-owner')).toBe(owner);
  } finally {
    pushState.mockRestore();
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

test('transplant delivers ordered presentation updates to retained panels', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/workspace');
  document.body.innerHTML = liveMarkup({
    content: directContent('Global Workspace'),
    home: homeMarkup({panels: panelRootMarkup()}),
  });
  window.config = {appSubUrl: '', pageData: {extension: {id: 'example'}}};
  const update = vi.fn();
  const dispose = vi.fn();
  let mountedResolve;
  const mountedPromise = new Promise((resolve) => { mountedResolve = resolve });
  const importer = vi.fn(async () => ({mount: () => {
    mountedResolve();
    return {dispose, update};
  }}));
  const adminPageData = {adminUserListSearchForm: {SortType: 'name'}};
  const fetcher = vi.fn(async () => navigationResponse(nativeDocument({title: 'Users', pageData: adminPageData,
    content: plainAdminContent('<form></form>'), home: homeMarkup({hidden: true})})));
  try {
    initExtensionPages(importer);
    await mountedPromise;
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(importer).toHaveBeenCalledOnce();
    initExtensionWorkspace(fetcher);
    clickWorkspaceLink('/admin/users?sort=name');
    await vi.waitFor(() => expect(update).toHaveBeenCalledOnce());
    expect(update).toHaveBeenCalledWith({route: '/admin/users?sort=name', params: {sort: 'name'}, mode: 'panel', visible: true});
    expect(dispose).not.toHaveBeenCalled();
    expect(importer).toHaveBeenCalledOnce();
  } finally {
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

test('page owners persist a plain leg parked and readopt on return', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/workspace');
  document.body.innerHTML = liveMarkup({
    content: directContent(`Global Workspace${pageRootMarkup()}`),
    home: homeMarkup({panels: panelRootMarkup()}),
  });
  window.config = {appSubUrl: '', pageData: {extension: {id: 'example'}}};
  const pageRoot = document.querySelector('[data-extension-page]');
  const suspend = vi.fn();
  const resume = vi.fn();
  const update = vi.fn();
  const dispose = vi.fn();
  let mountedResolve;
  const mountedPromise = new Promise((resolve) => { mountedResolve = resolve });
  const importer = vi.fn(async () => ({mount: (root) => {
    if (root.hasAttribute('data-extension-page')) {
      mountedResolve();
      return {dispose, update, suspend, resume};
    }
    return {dispose: () => {}};
  }}));
  const fetcher = vi.fn(async (url) => navigationResponse(url.includes('/admin/users') ?
    nativeDocument({title: 'Users', pageData: {adminUserListSearchForm: {}}, content: plainAdminContent('<form></form>'), home: homeMarkup({hidden: true})}) :
    nativeDocument({title: 'Workspace', pageData: {extension: {id: 'example'}}, content: directContent(`Global Workspace${pageRootMarkup()}`), home: homeMarkup()})));
  try {
    initExtensionPages(importer);
    await mountedPromise;
    await new Promise((resolve) => setTimeout(resolve, 0));
    initExtensionWorkspace(fetcher);
    clickWorkspaceLink('/admin/users');
    await vi.waitFor(() => expect(window.location.pathname).toBe('/admin/users'));
    expect(suspend).toHaveBeenCalledOnce();
    expect(dispose).not.toHaveBeenCalled();
    expect(importer).toHaveBeenCalledTimes(2);
    expect(pageRoot.isConnected).toBe(true);
    expect(document.querySelector('[data-extension-page-park]').contains(pageRoot)).toBe(true);
    expect(document.querySelector('.page-content').contains(pageRoot)).toBe(false);
    clickWorkspaceLink('/workspace');
    await vi.waitFor(() => expect(window.location.pathname).toBe('/workspace'));
    await vi.waitFor(() => expect(resume).toHaveBeenCalledOnce());
    expect(resume).toHaveBeenCalledWith({sessionGeneration: 'session-a'});
    expect(update).toHaveBeenCalledWith({route: '/workspace', params: {}, mode: 'page', visible: true});
    expect(dispose).not.toHaveBeenCalled();
    expect(importer).toHaveBeenCalledTimes(2);
    expect(document.querySelector('.page-content').contains(pageRoot)).toBe(true);
    expect(pageRoot.isConnected).toBe(true);
  } finally {
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

test('a changed contribution parks the live owner and mounts the entered root', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/workspace');
  document.body.innerHTML = liveMarkup({
    content: directContent(`Global Workspace${pageRootMarkup()}`),
    home: homeMarkup(),
  });
  window.config = {appSubUrl: '', pageData: {extension: {id: 'example'}}};
  const homeRoot = document.querySelector('[data-extension-page]');
  const firstDispose = vi.fn();
  const firstResume = vi.fn();
  const secondMount = vi.fn();
  secondMount.mockReturnValue({dispose: () => {}});
  let mountedResolve;
  const mountedPromise = new Promise((resolve) => { mountedResolve = resolve });
  const firstMount = () => {
    mountedResolve();
    return {dispose: firstDispose, resume: firstResume};
  };
  const importer = vi.fn(async () => ({mount: firstMount}));
  const fetcher = vi.fn(async (url) => navigationResponse(url.includes('/workspace/new') ?
    nativeDocument({title: 'New Project', pageData: {extension: {id: 'example'}},
      content: directContent(`New Project${pageRootMarkup({pageId: 'new'})}`), home: homeMarkup()}) :
    nativeDocument({title: 'Workspace', pageData: {extension: {id: 'example'}},
      content: directContent(`Global Workspace${pageRootMarkup()}`), home: homeMarkup()})));
  try {
    initExtensionPages(importer);
    await mountedPromise;
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(importer).toHaveBeenCalledOnce();
    importer.mockImplementationOnce(async () => ({mount: secondMount}));
    initExtensionWorkspace(fetcher);
    clickWorkspaceLink('/workspace/new');
    await vi.waitFor(() => expect(window.location.pathname).toBe('/workspace/new'));
    await vi.waitFor(() => expect(secondMount).toHaveBeenCalledOnce());
    expect(firstDispose).not.toHaveBeenCalled();
    expect(importer).toHaveBeenCalledTimes(2);
    expect(document.querySelector('[data-extension-page-park]').contains(homeRoot)).toBe(true);
    clickWorkspaceLink('/workspace');
    await vi.waitFor(() => expect(window.location.pathname).toBe('/workspace'));
    await vi.waitFor(() => expect(firstResume).toHaveBeenCalledOnce());
    expect(firstDispose).not.toHaveBeenCalled();
    expect(importer).toHaveBeenCalledTimes(2);
    expect(document.querySelector('.page-content').contains(homeRoot)).toBe(true);
  } finally {
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

test('leaves modified links, downloads and excluded destinations to native handling', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/workspace');
  document.body.innerHTML = liveMarkup({content: directContent('Global Workspace'), home: homeMarkup()});
  window.config = {appSubUrl: '', pageData: {extension: {id: 'example'}}};
  const fetcher = vi.fn(async () => navigationResponse(nativeDocument({title: 'Workspace', pageData: {},
    content: directContent(''), home: homeMarkup()})));
  try {
    initExtensionWorkspace(fetcher);
    const modified = document.createElement('a');
    modified.href = '/admin/users';
    document.body.append(modified);
    modified.dispatchEvent(new MouseEvent('click', {bubbles: true, cancelable: true, ctrlKey: true}));
    expect(fetcher).not.toHaveBeenCalled();
    modified.remove();
    const download = document.createElement('a');
    download.href = '/admin/users';
    download.download = 'users';
    document.body.append(download);
    download.dispatchEvent(new MouseEvent('click', {bubbles: true, cancelable: true}));
    expect(fetcher).not.toHaveBeenCalled();
    download.remove();
    const blocked = document.createElement('a');
    blocked.href = '/-/native/session';
    document.body.append(blocked);
    const rejected = new MouseEvent('click', {bubbles: true, cancelable: true});
    blocked.dispatchEvent(rejected);
    expect(rejected.defaultPrevented).toBe(true);
    expect(fetcher).not.toHaveBeenCalled();
    blocked.remove();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(document.querySelector('.page-content').textContent).toBe('Global Workspace');
  } finally {
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

test('takes a full document load for unknown layouts', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/workspace');
  document.body.innerHTML = liveMarkup({content: directContent('Global Workspace'), home: homeMarkup()});
  window.config = {appSubUrl: '', pageData: {extension: {id: 'example'}}};
  const fetcher = vi.fn(async () => navigationResponse(nativeDocument({title: 'Workspace', pageData: {},
    content: directContent(''), home: homeMarkup()})));
  const assign = vi.spyOn(window.location, 'assign').mockImplementation(() => {});
  try {
    initExtensionWorkspace(fetcher);
    clickWorkspaceLink('/team/repo/issues');
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(fetcher).not.toHaveBeenCalled();
    expect(assign).toHaveBeenCalledWith(expect.stringContaining('/team/repo/issues'));
    expect(document.querySelector('.page-content').textContent).toBe('Global Workspace');
  } finally {
    assign.mockRestore();
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

test('workspace uses its declared subpath for session checks while retaining the native deep link', async () => {
  document.body.innerHTML = liveMarkup({content: directContent('Global Workspace'), home: homeMarkup({appSubURL: '/forge'})});
  window.history.replaceState(null, '', '/forge/workspace#comment-1');
  const fetcher = vi.fn().mockResolvedValue({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-a'})});
  initExtensionWorkspace(fetcher);
  window.dispatchEvent(new Event('focus'));
  await vi.waitFor(() => expect(fetcher).toHaveBeenCalledWith(`${window.location.origin}/forge/-/native/session`, {
    credentials: 'same-origin', cache: 'no-store', redirect: 'manual',
  }));
  expect(window.location.pathname + window.location.search + window.location.hash).toBe('/forge/workspace#comment-1');
  window.dispatchEvent(new Event('pagehide'));
});

test('rechecks a restored document and retires owners on account change', async () => {
  document.body.innerHTML = liveMarkup({
    content: directContent('Global Workspace'),
    home: homeMarkup({panels: panelRootMarkup()}),
  });
  window.history.replaceState(null, '', '/workspace');
  const home = document.querySelector('[data-extension-panels]');
  const dispose = vi.fn();
  const pagesFetcher = vi.fn().mockResolvedValue({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-a'})});
  initExtensionPages(async () => ({mount: () => dispose}), pagesFetcher);
  await new Promise((resolve) => setTimeout(resolve, 0));
  const fetcher = vi.fn().mockResolvedValueOnce({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-a'})})
    .mockResolvedValueOnce({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-b'})});
  initExtensionWorkspace(fetcher);
  const cached = new Event('pagehide');
  Object.defineProperty(cached, 'persisted', {value: true});
  window.dispatchEvent(cached);
  const restored = new Event('pageshow');
  Object.defineProperty(restored, 'persisted', {value: true});
  window.dispatchEvent(restored);
  await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(dispose).not.toHaveBeenCalled();
  expect(document.querySelector('.page-content').textContent).toBe('Global Workspace');
  expect(fetcher).toHaveBeenCalledWith(`${window.location.origin}/-/native/session`, {
    credentials: 'same-origin', cache: 'no-store', redirect: 'manual',
  });
  window.dispatchEvent(new Event('focus'));
  await vi.waitFor(() => expect(document.querySelector('.page-content').textContent).toContain('Your session changed'));
  expect(home.hasAttribute('hidden')).toBe(true);
  // The retired panel root stays as inert markup; its owner is gone.
  expect(home.querySelector('[data-extension-panel]')).not.toBeNull();
  expect(dispose).toHaveBeenCalledOnce();
  window.dispatchEvent(new Event('pagehide'));
});

test('ignores a session check completed after the document was cached', async () => {
  document.body.innerHTML = liveMarkup({content: directContent('Global Workspace'), home: homeMarkup()});
  window.history.replaceState(null, '', '/workspace');
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
  expect(document.querySelector('.page-content').textContent).toBe('Global Workspace');
  resolveCurrent({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-a'})});
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(document.querySelector('.page-content').textContent).toBe('Global Workspace');
  window.dispatchEvent(new Event('pagehide'));
});

// NFX-A01/H-0, proved by NFX-A02: both entry orders run the real transplant
// chain across the direct contribution shape and the plain native shape,
// keeping one retained owner while the live page config follows each page.
test('contribution -> native -> contribution refreshes live page config with one retained owner', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/workspace');
  document.body.innerHTML = liveMarkup({
    content: directContent('Global Workspace'),
    home: homeMarkup({panels: '<div id="notes-owner">notes</div>'}),
  });
  window.config = {appSubUrl: '', pageData: {extension: {id: 'example'}}};
  const owner = document.getElementById('notes-owner');
  const adminPageData = {adminUserListSearchForm: {SortType: 'name'}};
  const workspacePageData = {extension: {id: 'example'}};
  const fetcher = vi.fn(async (url) => navigationResponse(url.includes('/admin/users') ?
    nativeDocument({title: 'Users - Forgejo', pageData: adminPageData, content: plainAdminContent('<form id="user-list-search-form"></form>'), home: homeMarkup({hidden: true})}) :
    nativeDocument({title: 'Workspace', pageData: workspacePageData, content: directContent('Global Workspace'), home: homeMarkup()})));
  try {
    initExtensionWorkspace(fetcher);
    clickWorkspaceLink('/admin/users');
    await vi.waitFor(() => expect(window.location.pathname).toBe('/admin/users'));
    expect(document.title).toBe('Users - Forgejo');
    expect(document.querySelector('#user-list-search-form')).not.toBeNull();
    expect(window.config.pageData).toEqual(adminPageData);
    expect(document.getElementById('notes-owner')).toBe(owner);
    clickWorkspaceLink('/workspace');
    await vi.waitFor(() => expect(window.location.pathname).toBe('/workspace'));
    expect(document.title).toBe('Workspace');
    expect(window.config.pageData).toEqual(workspacePageData);
    expect(document.getElementById('notes-owner')).toBe(owner);
    expect(owner.isConnected).toBe(true);
  } finally {
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

// region_adopt: plain native documents carry the hidden home, so native-first
// order transplants immediately without a full load. Documents with no home
// (login and other ineligible shapes) stay fully native.
test('plain native documents transplant from the hidden home', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/admin/users');
  document.body.innerHTML = liveMarkup({
    content: plainAdminContent('<form id="user-list-search-form"></form>'),
    home: homeMarkup({hidden: true, panels: panelRootMarkup()}),
  });
  window.config = {appSubUrl: '', pageData: {adminUserListSearchForm: {SortType: 'name'}}};
  const home = document.querySelector('[data-extension-panels]');
  const workspacePageData = {extension: {id: 'example'}};
  const fetcher = vi.fn(async () => navigationResponse(nativeDocument({title: 'Workspace', pageData: workspacePageData,
    content: directContent(`Global Workspace${pageRootMarkup()}`), home: homeMarkup()})));
  try {
    initExtensionWorkspace(fetcher);
    expect(home.hasAttribute('hidden')).toBe(true);
    clickWorkspaceLink('/workspace');
    await vi.waitFor(() => expect(window.location.pathname).toBe('/workspace'));
    expect(fetcher).toHaveBeenCalledOnce();
    expect(document.title).toBe('Workspace');
    expect(window.config.pageData).toEqual(workspacePageData);
    expect(document.querySelector('[data-extension-panels]')).toBe(home);
    expect(home.hasAttribute('hidden')).toBe(false);
    expect(document.querySelector('.page-content').textContent).toContain('Global Workspace');
  } finally {
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

test('documents without a home stay fully native', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/admin/users');
  document.body.innerHTML = '<nav id="navbar"></nav><div class="full height"><div role="main" class="page-content admin"><div class="ui container flex-container"><div class="flex-container-main"><form id="user-list-search-form"></form></div></div></div></div><footer></footer>';
  window.config = {appSubUrl: '', pageData: {adminUserListSearchForm: {SortType: 'name'}}};
  const fetcher = vi.fn();
  try {
    initExtensionWorkspace(fetcher);
    expect(document.querySelector('[data-extension-panels]')).toBeNull();
    const link = document.createElement('a');
    link.href = '/workspace';
    document.body.append(link);
    const event = new MouseEvent('click', {bubbles: true, cancelable: true});
    link.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
    expect(fetcher).not.toHaveBeenCalled();
    link.remove();
  } finally {
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

test('retires the document when the entered home carries a foreign generation', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/workspace');
  document.body.innerHTML = liveMarkup({
    content: directContent(`Global Workspace${pageRootMarkup()}`),
    home: homeMarkup(),
  });
  window.config = {appSubUrl: '', pageData: {extension: {id: 'example'}}};
  const fetcher = vi.fn(async () => navigationResponse(
    nativeDocument({title: 'Workspace', pageData: {extension: {id: 'example'}},
      content: directContent(`Foreign${pageRootMarkup({generation: 'session-b'})}`), home: homeMarkup({generation: 'session-b'})}),
    'session-b'));
  const invalidated = vi.fn();
  window.addEventListener('extension-workspace-invalidated', invalidated);
  try {
    initExtensionWorkspace(fetcher);
    clickWorkspaceLink('/workspace?view=all');
    await vi.waitFor(() => expect(document.querySelector('.page-content').textContent).toContain('Your session changed'));
    expect(document.querySelector('[data-extension-page]')).toBeNull();
    expect(document.querySelector('[data-extension-panels]').hasAttribute('hidden')).toBe(true);
    expect(invalidated).toHaveBeenCalledOnce();
    expect(window.config.pageData).toEqual({extension: {id: 'example'}});
  } finally {
    window.removeEventListener('extension-workspace-invalidated', invalidated);
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

test('transplants same-origin GET form submits and leaves POST forms native', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/workspace');
  document.body.innerHTML = liveMarkup({
    content: directContent('<form id="user-list-search-form" method="get" action="/admin/users"><input type="search" name="q" value="alice"><button type="submit" name="sort" value="email">Sort</button></form>'),
    home: homeMarkup(),
  });
  window.config = {appSubUrl: '', pageData: {extension: {id: 'example'}}};
  const adminPageData = {adminUserListSearchForm: {SortType: 'email'}};
  const fetcher = vi.fn(async () => navigationResponse(nativeDocument({title: 'Users', pageData: adminPageData,
    content: plainAdminContent('<form id="user-list-search-form"></form>'), home: homeMarkup({hidden: true})})));
  try {
    initExtensionWorkspace(fetcher);
    document.querySelector('#user-list-search-form').requestSubmit(document.querySelector('button[name="sort"]'));
    await vi.waitFor(() => expect(window.location.pathname + window.location.search).toBe('/admin/users?q=alice&sort=email'));
    expect(fetcher).toHaveBeenCalledOnce();
    expect(window.config.pageData).toEqual(adminPageData);
    const post = document.createElement('form');
    post.method = 'post';
    post.action = '/admin/users';
    document.body.append(post);
    const event = new Event('submit', {bubbles: true, cancelable: true});
    post.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
    expect(fetcher).toHaveBeenCalledOnce();
    post.remove();
  } finally {
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

test('brackets transplants with retire, config refresh and init in order', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/workspace');
  document.body.innerHTML = liveMarkup({content: directContent('Global Workspace'), home: homeMarkup()});
  window.config = {appSubUrl: '', pageData: {extension: {id: 'example'}}};
  const order = [];
  let observedConfig = null;
  registerPageFeature('a02-bracketing-probe', {
    init: () => {
      order.push('init');
      observedConfig = window.config.pageData;
    },
    retire: () => {
      order.push('retire');
    },
  });
  const adminPageData = {adminUserListSearchForm: {SortType: 'name'}};
  const workspacePageData = {extension: {id: 'example'}};
  const fetcher = vi.fn(async (url) => navigationResponse(url.includes('/admin/users') ?
    nativeDocument({title: 'Users', pageData: adminPageData, content: plainAdminContent('<form></form>'), home: homeMarkup({hidden: true})}) :
    nativeDocument({title: 'Workspace', pageData: workspacePageData, content: directContent('Global Workspace'), home: homeMarkup()})));
  try {
    initExtensionWorkspace(fetcher);
    clickWorkspaceLink('/admin/users');
    await vi.waitFor(() => expect(window.location.pathname).toBe('/admin/users'));
    expect(order).toEqual(['init']);
    expect(observedConfig).toEqual(adminPageData);
    clickWorkspaceLink('/workspace');
    await vi.waitFor(() => expect(window.location.pathname).toBe('/workspace'));
    expect(order).toEqual(['init', 'retire', 'init']);
  } finally {
    retirePageFeatures();
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});
