import $ from 'jquery';
import {htmlEscape} from 'escape-goat';
import {createCodeEditor, destroyCodeEditor} from './codeeditor.ts';
import {hideElem, showElem, createElementFromHTML} from '../utils/dom.js';
import {initMarkupContent} from '../markup/content.js';
import {attachRefIssueContextPopup} from './contextpopup.js';
import {POST} from '../modules/fetch.js';
import {initTab} from '../modules/tab.ts';
import {showModal} from '../modules/modal.ts';
import {contributionRootSelector, currentPageGeneration} from '../native-lifecycle.js';

// Page-lifetime state for the repository editor. Every listener and the
// CodeMirror view created by init are released by retire; re-init after
// retire rebuilds them against the entered document.
let editorView = null;
let editorGeneration = -1;
let previewHandler = null;
let previewTabEl = null;
let quickPullEls = [];
let filenameEl = null;
let commitButtonEl = null;
let commitHandler = null;

function onPreviewClick(form) {
  return async function () {
    const generation = editorGeneration;
    const $this = $(this);
    let context = `${$this.data('context')}/`;
    const mode = $this.data('markup-mode') || 'comment';
    const $treePathEl = form.find('input#tree_path');
    if ($treePathEl.length > 0) context += $treePathEl.val();
    context = context.substring(0, context.lastIndexOf('/'));
    const formData = new FormData();
    formData.append('mode', mode);
    formData.append('context', context);
    formData.append('branch_path', $this.data('branch-path'));
    formData.append('text', form.find(`.tab[data-tab="${form.find('.switch').data('write')}"] textarea`).val());
    formData.append('file_path', $treePathEl.val());
    try {
      const response = await POST($this.data('url'), {data: formData});
      const data = await response.text();
      if (generation !== currentPageGeneration()) return;
      renderPreviewPanelContent(form.find(`.tab[data-tab="${form.find('.switch').data('preview')}"]`), data);
    } catch (error) {
      if (generation === currentPageGeneration()) console.error('Error:', error);
    }
  };
}

function initEditPreviewTab($form) {
  const $tabMenu = $form.find('.switch');
  if (!$tabMenu.length) return;
  initTab($tabMenu[0]);
  const $previewTab = $tabMenu.find(`.item[data-tab="${$tabMenu.data('preview')}"]`);
  if ($previewTab.length) {
    previewTabEl = $previewTab[0];
    previewHandler = onPreviewClick($form);
    $previewTab.on('click', previewHandler);
  }
}

function getCursorPosition($e) {
  const el = $e.get(0);
  let pos = 0;
  if ('selectionStart' in el) {
    pos = el.selectionStart;
  } else if ('selection' in document) {
    el.focus();
    const Sel = document.selection.createRange();
    const SelLength = document.selection.createRange().text.length;
    Sel.moveStart('character', -el.value.length);
    pos = Sel.text.length - SelLength;
  }
  return pos;
}

function joinTreePath($fileNameEl) {
  const parts = [];
  $('.breadcrumb span.section').each(function () {
    const $element = $(this);
    if ($element.find('a').length) parts.push($element.find('a').text());
    else parts.push($element.text());
  });
  if ($fileNameEl.val()) parts.push($fileNameEl.val());
  $('#tree_path').val(parts.join('/'));
}

function onQuickPullChange() {
  if ($(this).val() === 'commit-to-new-branch') {
    showElem('.quick-pull-branch-name');
    const input = document.querySelector('.quick-pull-branch-name input');
    if (input) input.required = true;
  } else {
    hideElem('.quick-pull-branch-name');
    const input = document.querySelector('.quick-pull-branch-name input');
    if (input) input.required = false;
  }
  const btn = document.getElementById('commit-button');
  if (btn) btn.textContent = this.getAttribute('button_text');
}

function onFilenameInput() {
  const parts = $(this).val().split('/');
  if (parts.length > 1) {
    for (let i = 0; i < parts.length; ++i) {
      const value = parts[i];
      if (i < parts.length - 1) {
        if (value.length) {
          this.before(createElementFromHTML(`<span class="section"><a href="#">${htmlEscape(value)}</a></span>`));
          this.before(createElementFromHTML(`<div class="breadcrumb-divider">/</div>`));
        }
      } else {
        $(this).val(value);
      }
      this.setSelectionRange(0, 0);
    }
  }
  joinTreePath($(this));
}

function onFilenameKeydown(e) {
  const $section = $('.breadcrumb span.section');
  if (e.code === 'Backspace' && getCursorPosition($(this)) === 0 && $section.length > 0) {
    e.preventDefault();
    const $divider = $('.breadcrumb .breadcrumb-divider');
    const value = $section.last().find('a').text();
    $(this).val(value + $(this).val());
    this.setSelectionRange(value.length, value.length);
    $section.last().remove();
    $divider.last().remove();
    joinTreePath($(this));
  }
}

export function initRepoEditor() {
  const $form = $('.repository .edit.form');
  if ($form.length && $form[0].closest(contributionRootSelector)) return;
  retireRepoEditor();
  editorGeneration = currentPageGeneration();
  const generation = editorGeneration;
  initEditPreviewTab($form);

  quickPullEls = Array.from(document.querySelectorAll('.js-quick-pull-choice-option'));
  for (const el of quickPullEls) el.addEventListener('change', onQuickPullChange);

  const $editFilename = $('#file-name');
  filenameEl = $editFilename[0] ?? null;
  if (filenameEl) {
    filenameEl.addEventListener('input', onFilenameInput);
    filenameEl.addEventListener('keydown', onFilenameKeydown);
  }

  const $editArea = $('.repository.editor textarea#edit_area');
  if (!$editArea.length || !filenameEl) return;

  (async () => {
    const editor = await createCodeEditor($editArea[0], filenameEl);
    // The page retired while CodeMirror loaded: destroy the fresh view
    // instead of wiring a detached editor.
    if (generation !== currentPageGeneration()) {
      destroyCodeEditor(editor);
      return;
    }
    editorView = editor;
    const commitButton = document.getElementById('commit-button');
    commitButtonEl = commitButton;
    const $editForm = $('.ui.edit.form');
    const dirtyFileClass = 'dirty-file';
    if ($('input[name="page_has_posted"]').val() !== 'true' && commitButton) {
      commitButton.disabled = true;
    }
    $editForm.areYouSure({
      silent: true,
      dirtyClass: dirtyFileClass,
      fieldSelector: ':input:not(.commit-form-wrapper :input)',
      change($form2) {
        const dirty = $form2[0]?.classList.contains(dirtyFileClass);
        if (commitButton) commitButton.disabled = !dirty;
      },
    });
    const params = new URLSearchParams(window.location.search);
    const value = params.get('value');
    if (value) editor.setValue(value);
    commitHandler = (e) => {
      if (!$editArea.val()) {
        e.preventDefault();
        showModal('edit-empty-content-modal', () => { document.querySelector('.edit.form')?.requestSubmit() });
      }
    };
    commitButton?.addEventListener('click', commitHandler);
  })();
}

export function retireRepoEditor() {
  if (previewTabEl && previewHandler) $(previewTabEl).off('click', previewHandler);
  previewTabEl = null;
  previewHandler = null;
  for (const el of quickPullEls) el.removeEventListener('change', onQuickPullChange);
  quickPullEls = [];
  if (filenameEl) {
    filenameEl.removeEventListener('input', onFilenameInput);
    filenameEl.removeEventListener('keydown', onFilenameKeydown);
    filenameEl = null;
  }
  if (commitButtonEl && commitHandler) commitButtonEl.removeEventListener('click', commitHandler);
  commitButtonEl = null;
  commitHandler = null;
  if (editorView) destroyCodeEditor(editorView);
  editorView = null;
  editorGeneration = -1;
  // areYouSure binds document-level dirty tracking per form; its handlers
  // live on the outgoing form element and die with the region swap.
}

export function renderPreviewPanelContent($panelPreviewer, data) {
  $panelPreviewer.html(data);
  initMarkupContent();
  const $refIssues = $panelPreviewer.find('p .ref-issue');
  attachRefIssueContextPopup($refIssues);
}
