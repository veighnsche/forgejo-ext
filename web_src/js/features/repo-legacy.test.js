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

const {initRepoCommentForm, retireRepoCommentForm} = await import('./repo-legacy.js');

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
  document.body.innerHTML = '';
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
