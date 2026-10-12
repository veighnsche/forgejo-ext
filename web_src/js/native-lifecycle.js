// Native page-feature lifecycle seam (lane B).
//
// The H-0 contract with lane A: A's transition mechanism calls
// retirePageFeatures() before swapping native content, then setPageConfig()
// with the incoming document's page data and initPageFeatures() after.
// Global features initialize once per document and are never retired by
// navigation. Registered page features must not double-initialize: re-entry
// without retire is a no-op, and retire runs in reverse registration order.

const pageFeatures = new Map();
const globalFeatures = new Map();
const activePageFeatures = new Set();
let globalsInitialized = false;
let pageGeneration = 0;
let nativeNavigator = null;

export function registerPageFeature(name, {init, retire}) {
  if (pageFeatures.has(name)) throw new Error(`native feature "${name}" is already registered`);
  if (typeof init !== 'function') throw new Error(`native feature "${name}" needs an init function`);
  if (retire !== undefined && typeof retire !== 'function') throw new Error(`native feature "${name}" needs a retire function or none`);
  pageFeatures.set(name, {init, retire});
}

export function registerGlobalFeature(name, init) {
  if (globalFeatures.has(name)) throw new Error(`native feature "${name}" is already registered`);
  if (typeof init !== 'function') throw new Error(`native feature "${name}" needs an init function`);
  globalFeatures.set(name, init);
  // A global registered after document init still needs its one initialization.
  if (globalsInitialized) init();
}

export function initGlobalFeatures() {
  if (globalsInitialized) return;
  globalsInitialized = true;
  for (const init of globalFeatures.values()) {
    init();
  }
}

export function initPageFeatures() {
  for (const [name, feature] of pageFeatures) {
    if (activePageFeatures.has(name)) continue;
    feature.init();
    activePageFeatures.add(name);
  }
}

export function retirePageFeatures() {
  let firstError;
  for (const [name, feature] of Array.from(pageFeatures).reverse()) {
    if (!activePageFeatures.has(name) || typeof feature.retire !== 'function') {
      activePageFeatures.delete(name);
      continue;
    }
    try {
      feature.retire();
    } catch (error) {
      firstError ??= error;
    }
    activePageFeatures.delete(name);
  }
  pageGeneration++;
  if (firstError) throw firstError;
}

// Replaces the per-document page data wholesale: stale keys from the previous
// document must not survive. Only per-page fields move here; document-stable
// window.config values (appUrl, i18n, ...) stay untouched.
export function setPageConfig(pageData) {
  window.config.pageData = pageData ?? {};
}

export function getPageData() {
  return window.config.pageData;
}

// Monotonic counter bumped by every retire. Async callbacks capture it at
// start and drop their result when it no longer matches.
export function currentPageGeneration() {
  return pageGeneration;
}

// The host transition plugs its transplant here; sort-style native
// navigations then stay in-shell. Plain pages without a host keep the native
// history-replacing load.
export function setNativeNavigator(navigate) {
  if (navigate !== null && typeof navigate !== 'function') throw new Error('native navigator must be a function or null');
  nativeNavigator = navigate;
}

export function navigateNative(url) {
  if (nativeNavigator) nativeNavigator(url);
  else window.location.replace(url);
}
