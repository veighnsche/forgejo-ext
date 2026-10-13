import $ from 'jquery';
import {vi} from 'vitest';

// The are-you-sure vendor plugin (and webpack in production) expects a
// global jQuery; provide it before the legacy module graph loads.
// eslint-disable-next-line no-jquery/variable-pattern
globalThis.jQuery = $;
vi.mock('../htmx.js', () => ({}));
vi.mock('htmx.org', () => ({default: {process: vi.fn(), config: {}}}));
vi.mock('./comp/ComboMarkdownEditor.js', () => ({
  getComboMarkdownEditor: () => null,
  initComboMarkdownEditor: vi.fn(async () => ({})),
}));
vi.mock('./comp/ReactionSelector.js', () => ({initCompReactionSelector: vi.fn()}));

const {initRepoCommentForm, retireRepoCommentForm, initRepository, retireRepository} = await import('./repo-legacy.js');
const {initCompReactionSelector} = await import('./comp/ReactionSelector.js');

function installCommentForm() {
  document.body.innerHTML = `<form class="comment form">
    <div class="ui floating jump select-label dropdown">
      <div class="filter menu" data-id="#label_ids">
        <a class="no-select item" href="#">Clear</a>
        <a class="item" href="#" data-id="5" data-id-selector="#label_5"><span class="octicon-check tw-invisible"></span>bug</a>
      </div>
    </div>
    <div class="ui labels list">
      <span class="no-select">No labels</span>
      <a class="item tw-hidden" id="label_5">bug</a>
    </div>
    <input type="hidden" id="label_ids" value="">
    <input type="text" name="title" value="draft title">
  </form>`;
  return document.querySelector('form.comment.form');
}

afterEach(() => {
  retireRepoCommentForm();
  retireRepository();
  document.body.innerHTML = '';
  vi.clearAllMocks();
});

test('label list-submit toggles once per click and detaches on retire', () => {
  const form = installCommentForm();
  initRepoCommentForm();
  initRepoCommentForm();
  const item = form.querySelector('.select-label .menu .item[data-id="5"]');
  item.click();
  // A stacked second binding would toggle the label straight back off.
  expect(item.classList.contains('checked')).toBe(true);
  expect(document.getElementById('label_5').classList.contains('tw-hidden')).toBe(false);
  expect(document.getElementById('label_ids').value).toBe('5');

  retireRepoCommentForm();
  item.click();
  expect(item.classList.contains('checked')).toBe(true);
  expect(document.getElementById('label_ids').value).toBe('5');
});

test('repository options toggles bind once and retire detaches', () => {
  document.body.innerHTML = `<div class="page-content repository"><div class="repository settings options">
    <input type="checkbox" class="enable-system" data-target="#wiki-box" checked><div id="wiki-box"></div>
  </div></div>`;
  initRepository();
  initRepository();
  const toggle = document.querySelector('.enable-system');
  const box = document.getElementById('wiki-box');
  toggle.checked = false;
  toggle.dispatchEvent(new Event('change', {bubbles: true}));
  expect(box.classList.contains('disabled')).toBe(true);
  retireRepository();
  toggle.checked = true;
  toggle.dispatchEvent(new Event('change', {bubbles: true}));
  expect(box.classList.contains('disabled')).toBe(true);
  // re-entry with fresh nodes rebinds
  document.body.innerHTML = `<div class="page-content repository"><div class="repository settings options">
    <input type="checkbox" class="enable-system" data-target="#tracker-box" checked><div id="tracker-box"></div>
  </div></div>`;
  initRepository();
  const fresh = document.querySelector('.enable-system');
  fresh.checked = false;
  fresh.dispatchEvent(new Event('change', {bubbles: true}));
  expect(document.getElementById('tracker-box').classList.contains('disabled')).toBe(true);
});

test('milestone clear-date and compare show-form act once and retire detaches', () => {
  document.body.innerHTML = `<div class="page-content repository">
    <div class="repository new milestone"><button id="clear-date" type="button">clear</button><input id="deadline" value="2026-01-01"></div>
    <div class="repository compare pull"><div><button class="show-form" type="button">form</button></div><div class="pullrequest-form tw-hidden">form</div></div>
  </div>`;
  initRepository();
  initRepository();
  document.getElementById('clear-date').click();
  expect(document.getElementById('deadline').value).toBe('');
  document.querySelector('.show-form').click();
  expect(document.querySelector('.pullrequest-form').classList.contains('tw-hidden')).toBe(false);
  retireRepository();
  document.getElementById('deadline').value = '2026-02-02';
  document.getElementById('clear-date').click();
  expect(document.getElementById('deadline').value).toBe('2026-02-02');
});

test('contribution-owned repository roots are never bound', () => {
  document.body.innerHTML = `<div data-extension-panel><div class="page-content repository"><div class="repository settings options">
    <input type="checkbox" class="enable-system" data-target="#owned-box" checked><div id="owned-box"></div>
  </div></div></div>`;
  initRepository();
  const toggle = document.querySelector('.enable-system');
  toggle.checked = false;
  toggle.dispatchEvent(new Event('change', {bubbles: true}));
  expect(document.getElementById('owned-box').classList.contains('disabled')).toBe(false);
});

test('issue pages wire reactions with a page-owned abort signal', () => {
  document.body.innerHTML = `<div class="page-content repository"><div class="repository view issue"></div></div>`;
  initRepository();
  expect(initCompReactionSelector).toHaveBeenCalledOnce();
  const signal = initCompReactionSelector.mock.lastCall[1]?.signal;
  expect(signal).toBeInstanceOf(AbortSignal);
  expect(signal.aborted).toBe(false);
  retireRepository();
  expect(signal.aborted).toBe(true);
});

test('fresh comment form gains dirty-leave protection exactly once', () => {
  const form = installCommentForm();
  // The vendor plugin's field selector relies on browser selector
  // behavior that happy-dom does not reproduce, so assert the lifecycle
  // wiring: the plugin is applied to unseen forms only, never re-applied
  // where it would reset an in-progress draft's baseline.
  const areYouSure = vi.spyOn($.fn, 'areYouSure');
  try {
    initRepoCommentForm();
    initRepoCommentForm();
    expect(areYouSure).toHaveBeenCalledTimes(1);
    expect($(form).data('ays-orig-field-count')).toBeDefined();
  } finally {
    areYouSure.mockRestore();
  }
});
