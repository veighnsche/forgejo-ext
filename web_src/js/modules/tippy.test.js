import {vi} from 'vitest';
import {createTippy, initGlobalTooltips, showTemporaryTooltip} from './tippy.js';

const contributionAttributes = ['data-extension-panels', 'data-extension-page', 'data-extension-panel'];
let observer;

// Capture the real observer solely so each test can release its document watch.
function startAutomaticTooltips() {
  const observe = vi.spyOn(MutationObserver.prototype, 'observe');
  try {
    initGlobalTooltips();
    [observer] = observe.mock.contexts;
  } finally {
    observe.mockRestore();
  }
}

afterEach(() => {
  observer?.disconnect();
  observer = undefined;
  const instances = new Set(Array.from(document.querySelectorAll('*'), (el) => el._tippy).filter(Boolean));
  for (const instance of instances) instance.destroy?.();
  document.body.innerHTML = '';
  vi.restoreAllMocks();
});

test.each(contributionAttributes)('automatic initial and attribute handling preserves %s ownership', async (attribute) => {
  document.body.innerHTML = `<div ${attribute} data-tooltip-content="owner root" title="root title" aria-label="root label">
    <button id="owned" data-tooltip-content="owner content" title="owner title" aria-label="owner label"></button>
  </div><button id="native" data-tooltip-content="native content"></button>`;
  const root = document.querySelector(`[${attribute}]`);
  const owned = document.querySelector('#owned');
  const native = document.querySelector('#native');
  const ownerInstance = {setProps: vi.fn()};
  owned._tippy = ownerInstance;
  startAutomaticTooltips();

  expect(root.getAttribute('title')).toBe('root title');
  expect(root.getAttribute('aria-label')).toBe('root label');
  expect(owned.getAttribute('title')).toBe('owner title');
  expect(owned.getAttribute('aria-label')).toBe('owner label');
  expect(native.getAttribute('aria-label')).toBe('native content');

  root.setAttribute('title', 'changed root title');
  owned.setAttribute('title', 'changed owner title');
  owned.setAttribute('data-tooltip-content', 'changed owner content');
  native.setAttribute('title', 'changed native title');
  await vi.waitFor(() => expect(native._tippy?.props.content).toBe('changed native title'));
  expect(root.getAttribute('title')).toBe('changed root title');
  expect(root.getAttribute('aria-label')).toBe('root label');
  expect(owned.getAttribute('title')).toBe('changed owner title');
  expect(owned.getAttribute('aria-label')).toBe('owner label');
  expect(owned._tippy).toBe(ownerInstance);
  expect(ownerInstance.setProps).not.toHaveBeenCalled();
});

test.each(contributionAttributes)('automatic added-node binding skips %s roots and children', async (attribute) => {
  startAutomaticTooltips();
  const root = document.createElement('div');
  root.setAttribute(attribute, '');
  root.setAttribute('data-tooltip-content', 'owner root');
  root.setAttribute('title', 'owner root title');
  root.innerHTML = '<button data-tooltip-content="owner child" title="owner child title"></button>';
  const owned = root.querySelector('button');
  const native = document.createElement('button');
  native.setAttribute('data-tooltip-content', 'new native content');
  document.body.append(root, native);
  await vi.waitFor(() => expect(native.getAttribute('aria-label')).toBe('new native content'));
  root.dispatchEvent(new MouseEvent('mouseover', {bubbles: true}));
  owned.dispatchEvent(new MouseEvent('mouseover', {bubbles: true}));
  expect(root._tippy).toBeUndefined();
  expect(owned._tippy).toBeUndefined();
  expect(root.hasAttribute('aria-label')).toBe(false);
  expect(owned.hasAttribute('aria-label')).toBe(false);
  expect(root.getAttribute('title')).toBe('owner root title');
  expect(owned.getAttribute('title')).toBe('owner child title');
  native.dispatchEvent(new MouseEvent('mouseover', {bubbles: true}));
  expect(native._tippy?.props.content).toBe('new native content');
});

test.each(contributionAttributes)('a queued native hover cannot reconfigure a node moved under %s', async (attribute) => {
  document.body.innerHTML = `<div ${attribute}></div><button id="moving" data-tooltip-content="native content"></button>`;
  const root = document.querySelector(`[${attribute}]`);
  const moving = document.querySelector('#moving');
  startAutomaticTooltips();
  root.append(moving);
  const ownerInstance = {setProps: vi.fn()};
  moving._tippy = ownerInstance;
  moving.setAttribute('title', 'moved owner title');
  moving.setAttribute('aria-label', 'moved owner label');
  moving.dispatchEvent(new MouseEvent('mouseover', {bubbles: true}));
  const native = document.createElement('button');
  native.setAttribute('data-tooltip-content', 'observer reached native');
  document.body.append(native);
  await vi.waitFor(() => expect(native.getAttribute('aria-label')).toBe('observer reached native'));
  expect(moving._tippy).toBe(ownerInstance);
  expect(ownerInstance.setProps).not.toHaveBeenCalled();
  expect(moving.getAttribute('title')).toBe('moved owner title');
  expect(moving.getAttribute('aria-label')).toBe('moved owner label');
});

test('explicit tooltip APIs remain usable inside a contribution', () => {
  document.body.innerHTML = '<div data-extension-page><button></button></div>';
  const target = document.querySelector('button');
  const instance = createTippy(target, {content: 'explicit content', trigger: 'manual'});
  expect(target._tippy).toBe(instance);
  showTemporaryTooltip(target, 'explicit temporary content');
  expect(instance.props.content).toBe('explicit temporary content');
});
