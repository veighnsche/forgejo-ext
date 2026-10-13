import {retirePageFeatures, setPageConfig, initPageFeatures, setNativeNavigator} from '../native-lifecycle.js';
import {submitEventSubmitter} from '../utils/dom.js';
import {mountContribution} from './extension-mount.js';

function isAuthPath(pathname) {
  return /(?:^|\/)user\/(?:login|logout|sign_up|forgot_password|forget_password|reset_password|recover_account|activate|activate_email|two_factor|webauthn|oauth2|openid|link_account|link_account_signin|link_account_signup)(?:\/|$)/.test(pathname) ||
    /(?:^|\/)user\/settings\/(?:change_password|security|applications|keys)(?:\/|$)/.test(pathname) ||
    /(?:^|\/)(?:install|login\/oauth|login\/openid|oauth2|openid)(?:\/|$)/.test(pathname);
}

function hasCredentialQuery(url) {
  return Array.from(url.searchParams.keys()).some((key) => /^(?:token|password|secret|client_secret|authorization|auth|code|credential|session|api_key|private_key|.*_token)$/i.test(key));
}

/** Classify a browser destination for host-handled native navigation. */
export function classifyWorkspaceNavigation(input, base, origin, appSubPath = '') {
  let url;
  try {
    url = new URL(input, base);
  } catch {
    return {kind: 'reject'};
  }
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) return {kind: 'reject'};
  if (/%2f|%5c/i.test(url.pathname)) return {kind: 'reject'};
  if (appSubPath && !url.pathname.startsWith(`${appSubPath}/`)) return {kind: 'top', url: url.href};
  if (url.origin !== origin || isAuthPath(url.pathname) || hasCredentialQuery(url)) {
    return {kind: 'top', url: url.href};
  }
  if (/(?:^|\/)-\/native(?:\/|$)/.test(url.pathname)) return {kind: 'reject'};
  return {kind: 'native', url: url.href, path: `${url.pathname}${url.search}${url.hash}`};
}

export function workspacePath(input, base, origin, appSubPath = '') {
  if (!input?.startsWith('/') || input.startsWith('//')) return null;
  const result = classifyWorkspaceNavigation(input, base, origin, appSubPath);
  return result.kind === 'native' ? result.path : null;
}

function matchDirectPattern(path, pattern) {
  const parts = path.replace(/\/$/, '').split('/');
  const segments = pattern.split('/');
  if (parts.length !== segments.length) return false;
  return segments.every((segment, i) => {
    if (segment.startsWith('{') && segment.endsWith('}')) return parts[i] !== '' && parts[i].length <= 255;
    return segment === parts[i];
  });
}

/**
 * Report whether a pathname is a workspace root or matches one of the
 * declared generic direct patterns the host publishes from manifest
 * subroute declarations (NFX-C04/H-ROUTE), e.g.
 * "/workspace/requests/{request}". Parameter values are bounded opaque
 * segments: no product-ID shape rule is applied here. Declared patterns
 * also admit their repository form "/{owner}/{repo}/workspace/...": the
 * host registers those routes generically (repositorySubroute) and the
 * global owner's content routes never claim the repository namespace.
 */
export function workspaceDirectPath(pathname, appSubPath = '', patterns = []) {
  if (pathname.endsWith('/workspace')) return true;
  if (!pathname.startsWith(appSubPath)) return false;
  const rest = pathname.slice(appSubPath.length);
  if (!rest.startsWith('/')) return false;
  if (patterns.some((pattern) => typeof pattern === 'string' && matchDirectPattern(rest, pattern))) return true;
  const segments = rest.split('/').filter((segment) => segment !== '');
  if (segments.length < 4 || segments[2] !== 'workspace') return false;
  const candidate = `/workspace/${segments.slice(3).join('/')}`;
  return patterns.some((pattern) => typeof pattern === 'string' && matchDirectPattern(candidate, pattern));
}

const reservedTopSegments = new Set(['user', 'users', 'admin', 'org', 'orgs', 'explore', 'issues', 'pulls', 'milestones', 'notifications', 'search', 'api', '-', 'login', 'logout', 'assets', 'help', 'settings', 'workspace', 'tailnet']);

/**
 * Report whether a pathname has the native repository shape
 * "/{owner}/{repo}/..." past the installation subpath (NFX-C07). Reserved
 * top-level namespaces never count as an owner. Admission only selects
 * the in-shell transition; the transplant still validates the entered
 * document (panels home, page config, session generation) and falls back
 * to a full native load for any shape it cannot join.
 */
export function isRepositoryPath(pathname, appSubPath = '') {
  if (!pathname.startsWith(appSubPath)) return false;
  const rest = pathname.slice(appSubPath.length);
  if (!rest.startsWith('/')) return false;
  const segments = rest.split('/').filter((segment) => segment !== '');
  if (segments.length < 2) return false;
  if (reservedTopSegments.has(segments[0])) return false;
  return segments.every((segment) => segment.length <= 255 && !segment.includes('%'));
}

function directPatterns(home) {
  try {
    const parsed = JSON.parse(home?.dataset.extensionSubroutes ?? '[]');
    return Array.isArray(parsed) ? parsed.filter((pattern) => typeof pattern === 'string') : [];
  } catch {
    return [];
  }
}

const importExtension = (url) => import(/* webpackIgnore: true */ url);
const sessionCheckPath = (appSubPath) => `${appSubPath}/-/native/session`;

function validAppSubURL(value) {
  return typeof value === 'string' && (value === '' ||
    /^\/[^/\\?#\s]+(?:\/[^/\\?#\s]+)*$/.test(value) &&
    !/%2f|%5c/i.test(value) && !value.split('/').some((part) => part === '.' || part === '..'));
}

function readEnteredConfig(native) {
  const embedded = native.body?.dataset.pageData;
  // A missing embed means an unknown document: full load. A present but
  // empty embed (null pageData) transplants with {}: wholesale config
  // replace leaves no stale keys and feature inits no-op on absent keys.
  if (typeof embedded !== 'string' || !embedded) return null;
  try {
    const parsed = JSON.parse(embedded);
    if (parsed === null) return {};
    return parsed && typeof parsed === 'object' ? parsed : null;
  } catch {
    return null;
  }
}

function focusFragment(hash) {
  let id = hash.slice(1);
  try {
    id = decodeURIComponent(id);
  } catch {
    // Keep the raw fragment when it is not valid percent-encoding.
  }
  const anchor = document.getElementById(id);
  anchor?.scrollIntoView();
  if (anchor?.matches('a[href], button, input, select, textarea, [tabindex]')) anchor.focus({preventScroll: true});
}

// Adopted identity: same contribution plus the live session generation plus
// the browser contract version. Generation mismatch is session change
// (retire path), never adoption; version skew is refused the same way.
function pageIdentity(root) {
  const dataset = root.dataset;
  return [dataset.extensionId, dataset.extensionPageId, dataset.extensionApiBase, dataset.extensionSessionGeneration, dataset.extensionContractVersion].join('\0');
}

function ownerKey(root) {
  return root.dataset.extensionOwnerId ? `${root.dataset.extensionId}\0${root.dataset.extensionOwnerId}` : null;
}

// Loose identity without the contract version: finds entered duplicates
// that are otherwise identical to a parked owner, so version skew takes
// the retire+conceal path instead of adoption or a second mount.
function pageIdentityLoose(root) {
  const dataset = root.dataset;
  return [dataset.extensionId, dataset.extensionPageId, dataset.extensionApiBase, dataset.extensionSessionGeneration].join('\0');
}

function liveSessionMatches(url, generation, fetcher) {
  return Promise.resolve().then(() => fetcher(url, {credentials: 'same-origin', cache: 'no-store', redirect: 'manual'}))
    .then((response) => response.status === 204 && response.headers.get('X-Extension-Session-Generation') === generation)
    .catch(() => false);
}

export function initExtensionPages(importer = importExtension, fetcher = (...args) => window.fetch(...args)) {
  const home = document.querySelector('[data-extension-panels]');
  const roots = document.querySelectorAll('[data-extension-page], [data-extension-panel]');
  // A page-only manifest leaves the eligible home empty on native boot;
  // the host still listens so transplanted pages mount on entry.
  if (!roots.length && !home) return;
  const pageRoots = new Set(Array.from(roots).filter((root) => root.hasAttribute('data-extension-page')));
  // NFX-C04/H-OWNER: the host selects one retained instance per declared
  // shared owner. The linked panel root is that instance; a page root
  // for the same (extension, owner) is a presentation slot only and
  // never mounts a competing owner.
  const panelOwnerKeys = new Set(Array.from(roots).filter((root) => root.hasAttribute('data-extension-panel')).map(ownerKey).filter(Boolean));
  const mounted = [];
  const pending = new Map();
  let leaving = false;
  const settle = (owner) => {
    if (!owner) return;
    if (leaving) {
      owner.dispose();
      return;
    }
    mounted.push(owner);
    // Initial presentation: a panel mounted while the persistent home is
    // hidden (native-first load) starts non-visible, so the host tells it
    // immediately instead of leaving it to assume visible work until the
    // first navigation update. A shared owner mounted visible on one of
    // its direct paths presents in page mode (NFX-A04/H-C), so it is told
    // at once as well. Other visible mounts need no initial update: the
    // contract default already matches the host state.
    if (!owner.root.hasAttribute('data-extension-page')) {
      const route = `${window.location.pathname}${window.location.search}${window.location.hash}`;
      const repositoryId = owner.root.dataset.extensionRepositoryId;
      const facts = {
        route, params: Object.fromEntries(new URLSearchParams(window.location.search)),
        ...(repositoryId ? {repositoryId} : {}),
      };
      if (home?.hasAttribute('hidden')) {
        owner.update({...facts, mode: 'panel', visible: false});
      } else if (owner.root.dataset.extensionOwnerId &&
        workspaceDirectPath(window.location.pathname, owner.root.dataset.appSubUrl ?? '', directPatterns(home))) {
        owner.update({...facts, mode: 'page', visible: true});
      }
    }
  };
  function mountRoot(root) {
    if (leaving || root.hasAttribute('data-extension-mounted')) return;
    if (root.hasAttribute('data-extension-page') && panelOwnerKeys.has(ownerKey(root))) {
      root.setAttribute('data-extension-owner-presented', '');
      return;
    }
    root.setAttribute('data-extension-mounted', '');
    const controller = new AbortController();
    pending.set(controller, root);
    mountContribution(root, {importer, fetcher, signal: controller.signal}).then((owner) => {
      pending.delete(controller);
      settle(owner);
    });
  }
  for (const root of roots) mountRoot(root);
  // region_adopt: page roots persist across plain legs. Before the content
  // swap the workspace parks live page roots into a hidden shelter inside
  // the persistent panels home; the post-swap sync readopts them into the
  // entered duplicate (same identity reuses the owner, streams suspended
  // meanwhile). The shelter holds at most four owners; the oldest parked
  // root past four disposes, unless it carries data-extension-persist: a
  // persistent shared owner (the Workspace terminal once C04 joins it) is
  // never evicted silently, so the bound yields when every parked root
  // persists.
  let park = home?.querySelector('[data-extension-page-park]') ?? null;
  function parkPageRoots() {
    if (leaving) return;
    if (!park) {
      park = document.createElement('div');
      park.setAttribute('data-extension-page-park', '');
      park.setAttribute('hidden', '');
      (home ?? document.body).append(park);
    }
    for (const owner of mounted) {
      if (!pageRoots.has(owner.root) || !owner.root.isConnected || park.contains(owner.root)) continue;
      owner.suspend();
      park.append(owner.root);
    }
    while (park.children.length > 4) {
      const oldest = Array.from(park.children).find((root) => !root.hasAttribute('data-extension-persist'));
      if (!oldest) return;
      const owner = mounted.find((candidate) => candidate.root === oldest);
      oldest.remove();
      if (owner) {
        mounted.splice(mounted.indexOf(owner), 1);
        pageRoots.delete(owner.root);
        owner.dispose();
      }
    }
  }
  function abortPending() {
    for (const controller of pending.keys()) controller.abort();
    pending.clear();
  }
  function disposeRoots() {
    if (leaving) return;
    leaving = true;
    abortPending();
    for (const owner of mounted.splice(0)) owner.dispose();
    window.removeEventListener('extension-workspace-invalidated', retireRoots);
    window.removeEventListener('extension-workspace-will-navigate', parkPageRoots);
    window.removeEventListener('extension-workspace-navigation', onWorkspaceNavigation);
  }
  function retireRoots() {
    if (leaving) return;
    leaving = true;
    abortPending();
    for (const owner of mounted.splice(0)) owner.retire('session-changed');
    window.removeEventListener('extension-workspace-invalidated', retireRoots);
    window.removeEventListener('extension-workspace-will-navigate', parkPageRoots);
    window.removeEventListener('extension-workspace-navigation', onWorkspaceNavigation);
  }
  // Each transplant swaps the native content region. Parked page owners
  // whose contribution persists readopt into the entered duplicate
  // (identity triple plus generation must match; the entered duplicate is
  // discarded unmounted), receive the ordered update, and resume; parked
  // owners with no matching root stay suspended in the shelter but still
  // receive current facts as visible:false. Panels report the home's live
  // visibility. Owners whose root left the document without parking
  // dispose (pages) or retire (panels); genuinely new roots mount.
  // Adoption runs after native init: native features target native
  // content, never contribution roots, so init effects on the entered
  // duplicate are discarded with it while the adopted owner keeps state.
  function syncContributionRoots(facts) {
    if (leaving) return;
    for (const [controller, root] of Array.from(pending)) {
      if (!root.isConnected || root.hasAttribute('data-extension-page')) {
        controller.abort();
        pending.delete(controller);
      }
    }
    const entered = new Map();
    const enteredLoose = new Map();
    for (const root of document.querySelectorAll('[data-extension-page]')) {
      if (!entered.has(pageIdentity(root))) entered.set(pageIdentity(root), root);
      else if (!root.hasAttribute('data-extension-mounted')) mountRoot(root);
      if (!enteredLoose.has(pageIdentityLoose(root))) enteredLoose.set(pageIdentityLoose(root), root);
    }
    const panelsVisible = home ? !home.hasAttribute('hidden') : true;
    const owned = new Set();
    for (const owner of Array.from(mounted)) {
      const isPage = pageRoots.has(owner.root);
      if (isPage && park !== null && park.contains(owner.root)) {
        const duplicate = entered.get(pageIdentity(owner.root));
        if (duplicate && duplicate !== owner.root) {
          entered.delete(pageIdentity(owner.root));
          duplicate.replaceWith(owner.root);
          if (duplicate.dataset.extensionRepositoryId === undefined) delete owner.root.dataset.extensionRepositoryId;
          else owner.root.dataset.extensionRepositoryId = duplicate.dataset.extensionRepositoryId;
          owned.add(owner.root);
          owner.update({...facts, mode: 'page', visible: true});
          owner.resume({sessionGeneration: owner.root.dataset.extensionSessionGeneration});
          continue;
        }
        // Version skew: an otherwise identical entered root whose
        // contract version is missing or differs is never adopted and
        // never mounted; the parked owner retires and both roots leave
        // the document. Checked before any adoption or second mount.
        const skewed = enteredLoose.get(pageIdentityLoose(owner.root));
        if (skewed && skewed !== owner.root) {
          entered.delete(pageIdentity(skewed));
          entered.delete(pageIdentity(owner.root));
          skewed.remove();
          owner.root.remove();
          mounted.splice(mounted.indexOf(owner), 1);
          pageRoots.delete(owner.root);
          owner.retire('contribution-unavailable');
          continue;
        }
        owner.update({...facts, mode: 'page', visible: false});
        continue;
      }
      if (!owner.root.isConnected) {
        mounted.splice(mounted.indexOf(owner), 1);
        if (isPage) {
          pageRoots.delete(owner.root);
          owner.dispose();
        } else {
          owner.retire('contribution-unavailable');
        }
        continue;
      }
      owned.add(owner.root);
      // A linked shared owner's retained panel presents in page mode on
      // its owner's direct paths (NFX-C04/H-OWNER).
      const routePath = typeof facts.route === 'string' ? facts.route.split(/[?#]/)[0] : '';
      const ownerDirect = !isPage && Boolean(owner.root.dataset.extensionOwnerId) &&
        workspaceDirectPath(routePath, owner.root.dataset.appSubUrl ?? '', directPatterns(home));
      owner.update({...facts, mode: isPage || ownerDirect ? 'page' : 'panel', visible: isPage ? true : panelsVisible});
    }
    for (const root of entered.values()) {
      if (owned.has(root)) continue;
      pageRoots.add(root);
      mountRoot(root);
    }
  }
  function onWorkspaceNavigation(event) {
    syncContributionRoots(event.detail ?? {});
  }
  window.addEventListener('pagehide', (event) => { if (!event.persisted) disposeRoots(); });
  window.addEventListener('extension-workspace-invalidated', retireRoots);
  window.addEventListener('extension-workspace-will-navigate', parkPageRoots);
  window.addEventListener('extension-workspace-navigation', onWorkspaceNavigation);
  if (!pageRoots.size && !home) return;
  const firstPage = pageRoots.size ? pageRoots.values().next().value : null;
  const generation = firstPage?.dataset.extensionSessionGeneration ?? home?.dataset.extensionSessionGeneration;
  const appSubURL = firstPage?.dataset.appSubUrl ?? home?.dataset.appSubUrl;
  const checkURL = new URL(sessionCheckPath(appSubURL), window.location.origin).href;
  let checkVersion = 0;
  let pendingCheck;
  let invalidated = false;
  // Revalidation conceals the whole retained region at once: every live
  // page root plus the persistent home (which carries all panel roots and
  // the parked shelter). Display alone toggles, so connected DOM and
  // sockets survive a valid session; only a mismatch retires owners and
  // posts the notice.
  const concealTargets = () => {
    const targets = Array.from(pageRoots);
    if (home && !targets.includes(home)) targets.push(home);
    return targets;
  };
  const invalidate = () => {
    if (invalidated) return;
    invalidated = true;
    for (const root of concealTargets()) {
      root.replaceChildren();
      root.style.display = '';
      root.textContent = 'Your session changed. Reload Forgejo to continue.';
    }
    retireRoots();
  };
  const checkSession = () => {
    if (invalidated || pendingCheck) return;
    // One joined lifecycle: conceal and suspend retained private state
    // before revalidation, and resume it only after the live session
    // verifies against the admitted generation. Consumers never
    // interpret cache/visibility events themselves.
    for (const root of concealTargets()) root.style.display = 'none';
    for (const owner of mounted) owner.suspend();
    const version = ++checkVersion;
    pendingCheck = liveSessionMatches(checkURL, generation, fetcher).then((matches) => {
      if (version !== checkVersion) return;
      if (!matches) invalidate();
      else {
        for (const root of concealTargets()) root.style.display = '';
        for (const owner of mounted) owner.resume({sessionGeneration: generation});
      }
    }).finally(() => { if (version === checkVersion) pendingCheck = null; });
  };
  const onPageShow = (event) => { if (event.persisted) checkSession(); };
  const onVisibilityChange = () => {
    if (document.visibilityState === 'hidden') {
      for (const owner of mounted) owner.suspend();
      return;
    }
    if (document.visibilityState === 'visible') checkSession();
  };
  const onPageHide = (event) => {
    checkVersion++;
    pendingCheck = null;
    for (const root of concealTargets()) root.style.display = 'none';
    if (event.persisted) {
      for (const owner of mounted) owner.suspend();
      return;
    }
    window.removeEventListener('pagehide', onPageHide);
    window.removeEventListener('pageshow', onPageShow);
    window.removeEventListener('focus', checkSession);
    document.removeEventListener('visibilitychange', onVisibilityChange);
  };
  window.addEventListener('pagehide', onPageHide);
  window.addEventListener('pageshow', onPageShow);
  window.addEventListener('focus', checkSession);
  document.addEventListener('visibilitychange', onVisibilityChange);
  if (!generation || !validAppSubURL(appSubURL)) invalidate();
}

export function initExtensionWorkspace(fetcher = (...args) => window.fetch(...args)) {
  // region_adopt: every eligible document is a whole native document with
  // the persistent panels home in the shared layout. Transplants swap the
  // native content region; the home (and its panel roots) never moves.
  const home = document.querySelector('[data-extension-panels]');
  if (!home) return;
  const liveContent = () => document.querySelector('.page-content');
  let navigationVersion = 0;
  let navigationController = null;
  const base = window.location.href;
  const origin = window.location.origin;
  const appSubPath = home.dataset.appSubUrl;
  const generation = home.dataset.extensionSessionGeneration;
  let invalidated = false;
  let pendingCheck;
  let checkVersion = 0;

  function invalidate() {
    if (invalidated) return;
    invalidated = true;
    navigationVersion++;
    navigationController?.abort();
    home.setAttribute('hidden', '');
    home.setAttribute('inert', '');
    const content = liveContent();
    if (content) content.style.display = 'none';
    try {
      retirePageFeatures();
    } catch (error) {
      console.error('Could not retire native page features', error);
    } finally {
      if (content) {
        content.replaceChildren();
        content.textContent = 'Your session changed. Reload Forgejo to continue.';
        content.style.display = '';
      }
      window.dispatchEvent(new Event('extension-workspace-invalidated'));
    }
  }

  function checkSession() {
    if (invalidated || pendingCheck) return pendingCheck;
    const version = ++checkVersion;
    // Keep native private content hidden until the live session verifies;
    // the contribution lifecycle above suspends/resumes/retires owners
    // against the same admitted generation.
    const content = liveContent();
    if (content) content.style.display = 'none';
    const endpoint = new URL(sessionCheckPath(appSubPath), origin);
    pendingCheck = liveSessionMatches(endpoint.href, generation, fetcher)
      .then((matches) => {
        if (version !== checkVersion) return;
        if (!matches) {
          invalidate();
        } else if (content) {
          content.style.display = '';
        }
      })
      .finally(() => {
        if (version === checkVersion) pendingCheck = null;
      });
    return pendingCheck;
  }

  if (!generation || !validAppSubURL(appSubPath)) {
    invalidate();
    return;
  }
  // A cached document rechecks the current native session on return.
  const onPageShow = (event) => { if (event.persisted) checkSession(); };
  const onVisibilityChange = () => { if (document.visibilityState === 'visible') checkSession(); };
  window.addEventListener('pageshow', onPageShow);
  window.addEventListener('focus', checkSession);
  document.addEventListener('visibilitychange', onVisibilityChange);
  // The live document is the current route; unknown layouts stay fully
  // native (transplant full-loads them) until their features migrate.
  let current = `${window.location.pathname}${window.location.search}${window.location.hash}`;

  // Transplantable layouts (NFX-C07): direct contributions (global and
  // repository workspace roots plus their declared subroutes), the /admin
  // dashboard and /admin/users, and native repository documents. Their
  // native features use the page seam; every other layout takes a full
  // document load until its features migrate.
  function transplantKind(path) {
    const pathname = new URL(path, origin).pathname;
    if (workspaceDirectPath(pathname, appSubPath, directPatterns(home))) return 'direct';
    if (pathname === `${appSubPath}/admin` || pathname === `${appSubPath}/admin/users`) return 'plain';
    if (isRepositoryPath(pathname, appSubPath)) return 'plain';
    return null;
  }

  // The persistent home is visible on direct contribution paths and
  // hidden+inert on ordinary native shapes; the server renders the initial
  // state and each transplant resyncs it. Hiding never drops focus to the
  // body: focus inside the home moves to the entered content region.
  // Showing never steals focus.
  function syncPanelsHome(path) {
    if (transplantKind(path) === 'direct') {
      home.removeAttribute('hidden');
      home.removeAttribute('inert');
      return;
    }
    const focusInside = home.contains(document.activeElement);
    home.setAttribute('hidden', '');
    home.setAttribute('inert', '');
    if (focusInside) {
      const live = liveContent();
      if (live) {
        live.setAttribute('tabindex', '-1');
        live.focus({preventScroll: true});
      }
    }
  }
  syncPanelsHome(current);
  // Public programmatic navigation (NFX-C04/H-ROUTE): contributions and
  // native features request transitions through the host instead of
  // assigning window.location. Admitted native paths transplant;
  // host-handled destinations take their native full load; private
  // native prefixes are refused.
  setNativeNavigator((url) => {
    const decision = classifyWorkspaceNavigation(url, base, origin, appSubPath);
    if (decision.kind === 'native') transplant(decision.path, true);
    else if (decision.kind === 'top') window.location.assign(decision.url);
  });

  // Active link states are per-page chrome. States copy across matching
  // links so listeners on the live navbar survive the transition.
  function syncNavbarStates(native) {
    const live = document.getElementById('navbar');
    const incoming = native.getElementById('navbar');
    if (!live || !incoming) return;
    const incomingByHref = new Map();
    for (const link of incoming.querySelectorAll('a[href]')) {
      incomingByHref.set(new URL(link.getAttribute('href'), origin).href, link);
    }
    for (const link of live.querySelectorAll('a[href]')) {
      const peer = incomingByHref.get(new URL(link.getAttribute('href'), origin).href);
      if (!peer) continue;
      link.classList.toggle('active', peer.classList.contains('active'));
      if (peer.hasAttribute('aria-current')) link.setAttribute('aria-current', peer.getAttribute('aria-current'));
      else link.removeAttribute('aria-current');
    }
  }

  async function transplant(path, push = false) {
    const kind = transplantKind(path);
    if (!kind) {
      window.location.assign(path);
      return;
    }
    navigationController?.abort();
    navigationController = new AbortController();
    const version = ++navigationVersion;
    const signal = navigationController.signal;
    let timedOut = false;
    const timer = setTimeout(() => {
      timedOut = true;
      navigationController.abort();
    }, 15000);
    let response;
    try {
      response = await fetcher(new URL(path, origin).href, {
        credentials: 'same-origin', cache: 'no-store', redirect: 'error',
        headers: {Accept: 'text/html', 'X-Extension-Workspace-Navigation': '1'}, signal,
      });
    } catch {
      clearTimeout(timer);
      if (!invalidated && version === navigationVersion && (timedOut || !signal.aborted)) window.location.assign(path);
      return;
    }
    clearTimeout(timer);
    if (invalidated || version !== navigationVersion || signal.aborted) return;
    if (!response.ok) {
      window.location.assign(path);
      return;
    }
    // P1: verify the entered document belongs to the live session before
    // touching any retained state. A foreign generation means the cookie or
    // actor changed out from under the shell: retire, never join.
    if (response.headers.get('X-Extension-Session-Generation') !== generation) {
      invalidate();
      return;
    }
    let html;
    try {
      html = await response.text();
    } catch {
      if (!invalidated && version === navigationVersion) window.location.assign(path);
      return;
    }
    if (invalidated || version !== navigationVersion || signal.aborted) return;
    const native = new DOMParser().parseFromString(html, 'text/html');
    const live = liveContent();
    const content = native.querySelector('.page-content');
    const enteredHome = native.querySelector('[data-extension-panels]');
    const pageData = readEnteredConfig(native);
    if (!live || !content || !pageData || !enteredHome) {
      window.location.assign(path);
      return;
    }
    if (enteredHome.dataset.extensionSessionGeneration !== generation) {
      invalidate();
      return;
    }
    if (invalidated || version !== navigationVersion || signal.aborted) return;
    // Only Fountain's native content region is replaced. The persistent
    // panels home, parked page roots and terminal DOM never leave the
    // owning document.
    const repositoryId = content.querySelector('[data-extension-page]')?.dataset.extensionRepositoryId ?? '';
    // Snapshot outgoing scroll before retiring/swapping: replacing tall
    // content can clamp window.scrollY, so reading it afterwards loses
    // the position Back must restore.
    const outgoingScrollY = window.scrollY;
    try {
      // Park live page roots before the swap so owners persist the leg.
      window.dispatchEvent(new Event('extension-workspace-will-navigate'));
      retirePageFeatures();
      live.replaceChildren(...content.childNodes);
      live.className = content.className;
      syncNavbarStates(native);
      syncPanelsHome(path);
      setPageConfig(pageData);
      initPageFeatures();
    } catch {
      window.location.assign(path);
      return;
    }
    document.title = native.title;
    current = path;
    const target = new URL(path, origin);
    if (push) {
      window.history.replaceState({...window.history.state, scrollY: outgoingScrollY}, '', window.location.href);
      window.history.pushState({...window.history.state, scrollY: 0}, '', path);
      if (target.hash) focusFragment(target.hash);
      else window.scrollTo(0, 0);
    } else {
      updateHistory(path);
    }
    home.dispatchEvent(new CustomEvent('extension-workspace-navigation', {bubbles: true, detail: {
      route: `${target.pathname}${target.search}${target.hash}`,
      params: Object.fromEntries(target.searchParams),
      ...(repositoryId ? {repositoryId} : {}),
    }}));
  }

  function onWorkspaceClick(event) {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    const link = event.composedPath().find((node) => node?.localName === 'a' && 'href' in node);
    if (!link || link.hasAttribute('download') || (link.target && link.target !== '_self')) return;
    const decision = classifyWorkspaceNavigation(link.href, window.location.href, origin, appSubPath);
    if (decision.kind === 'reject') {
      event.preventDefault();
      return;
    }
    if (decision.kind !== 'native') return;
    event.preventDefault();
    transplant(decision.path, true);
  }
  document.addEventListener('click', onWorkspaceClick);

  // GET forms navigate like links: same-origin GET submits to transplantable
  // paths swap the content region. formFetchAction runs first (index.js order) and
  // claims .form-fetch-action forms; POST and targeted forms keep native load.
  function onWorkspaceSubmit(event) {
    if (event.defaultPrevented) return;
    const form = event.target;
    if (!(form instanceof HTMLFormElement)) return;
    if (form.target && form.target !== '_self') return;
    const submitter = submitEventSubmitter(event);
    const effectiveMethod = (submitter?.getAttribute('formmethod') || form.getAttribute('method') || 'get').toLowerCase();
    if (effectiveMethod !== 'get') return;
    const formData = new FormData(form);
    const submitterName = submitter?.getAttribute('name');
    if (submitterName) formData.append(submitterName, submitter.getAttribute('value') || '');
    const params = new URLSearchParams();
    for (const [key, value] of formData) params.append(key, value.toString());
    const target = new URL(form.getAttribute('action') || window.location.href, window.location.href);
    target.search = params.toString();
    const decision = classifyWorkspaceNavigation(target.href, window.location.href, origin, appSubPath);
    if (decision.kind !== 'native') return;
    event.preventDefault();
    transplant(decision.path, true);
  }
  document.addEventListener('submit', onWorkspaceSubmit);

  function updateHistory(path) {
    const state = window.history.state;
    window.history.replaceState(state, '', path);
  }

  const onPopState = () => {
    if (invalidated) return;
    navigationVersion++;
    const path = workspacePath(`${window.location.pathname}${window.location.search}${window.location.hash}`, base, origin, appSubPath);
    if (!path || path === current) return;
    transplant(path).then(() => {
      if (!invalidated) window.scrollTo(0, window.history.state?.scrollY ?? 0);
    });
  };
  window.addEventListener('popstate', onPopState);
  const onPageHide = (event) => {
    navigationVersion++;
    navigationController?.abort();
    checkVersion++;
    pendingCheck = null;
    if (event.persisted) {
      // Cached native content stays hidden until revalidation verifies it.
      const content = liveContent();
      if (content) content.style.display = 'none';
      return;
    }
    setNativeNavigator(null);
    document.removeEventListener('click', onWorkspaceClick);
    document.removeEventListener('submit', onWorkspaceSubmit);
    window.removeEventListener('pagehide', onPageHide);
    window.removeEventListener('pageshow', onPageShow);
    window.removeEventListener('focus', checkSession);
    window.removeEventListener('popstate', onPopState);
    document.removeEventListener('visibilitychange', onVisibilityChange);
  };
  window.addEventListener('pagehide', onPageHide);
  updateHistory(current);
}
