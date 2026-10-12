import $ from 'jquery';
import htmx from 'htmx.org';
import {initCompReactionSelector} from './comp/ReactionSelector.js';
import {initRepoIssueContentHistory} from './repo-issue-content.js';
import {initDiffFileTree, retireDiffFileTree} from './repo-diff-filetree.js';
import {initDiffCommitSelect, retireDiffCommitSelect} from './repo-diff-commitselect.js';
import {validateTextareaNonEmpty} from './comp/ComboMarkdownEditor.js';
import {initViewedCheckboxListenerFor, countAndUpdateViewedFiles, initExpandAndCollapseFilesButton, retireViewedFiles} from './pull-view-file.js';
import {initImageDiff, retireImageDiff} from './imagediff.js';
import {showErrorToast} from '../modules/toast.js';
import {submitEventSubmitter, queryElemSiblings, hideElem, showElem} from '../utils/dom.js';
import {POST, GET} from '../modules/fetch.js';
import {currentPageGeneration, getPageData} from '../native-lifecycle.js';

let diffAttached = false;
const reviewForms = new Set();
const toggleEls = new Set();

function onReviewButtonClick(e) {
  const reviewBox = document.getElementById('review-box');
  const counter = reviewBox?.querySelector('.review-comments-counter');
  if (!reviewBox || !counter) return;
  const form = e.target.closest('form');
  if (!form || reviewForms.has(form)) return;
  reviewForms.add(form);
  form.addEventListener('submit', () => {
    const num = parseInt(counter.getAttribute('data-pending-comment-number')) + 1 || 1;
    counter.setAttribute('data-pending-comment-number', String(num));
    counter.textContent = String(num);
    reviewBox.classList.remove('pulse');
    requestAnimationFrame(() => reviewBox.classList.add('pulse'));
  });
}

function initRepoDiffReviewButton() {
  if (!document.getElementById('review-box')) return;
  $(document).on('click', 'button[name="pending_review"]', onReviewButtonClick);
}

function onFileViewToggle() {
  for (const el of queryElemSiblings(this)) el.classList.remove('active');
  this.classList.add('active');
  const target = document.querySelector(this.getAttribute('data-toggle-selector'));
  if (!target) return;
  hideElem(queryElemSiblings(target));
  showElem(target);
}

function initRepoDiffFileViewToggle() {
  for (const el of document.querySelectorAll('.file-view-toggle')) {
    if (toggleEls.has(el)) continue;
    el.addEventListener('click', onFileViewToggle);
    toggleEls.add(el);
  }
}

async function onConversationSubmit(e) {
  e.preventDefault();
  const generation = currentPageGeneration();
  const $form = $(e.target);
  const textArea = e.target.querySelector('textarea');
  if (!validateTextareaNonEmpty(textArea)) return;
  if (e.target.classList.contains('is-loading')) return;
  try {
    e.target.classList.add('is-loading');
    const formData = new FormData($form[0]);
    const submitter = submitEventSubmitter(e.originalEvent ?? e);
    const isSubmittedByButton = (submitter?.nodeName === 'BUTTON') || (submitter?.nodeName === 'INPUT' && submitter.type === 'submit');
    if (isSubmittedByButton && submitter.name) formData.append(submitter.name, submitter.value);
    const response = await POST(e.target.getAttribute('action'), {data: formData});
    const $newConversationHolder = $(await response.text());
    if (generation !== currentPageGeneration()) return;
    const {path, side, idx} = $newConversationHolder.data();
    $form.closest('.conversation-holder').replaceWith($newConversationHolder);
    let selector;
    if ($form.closest('tr').data('line-type') === 'same') selector = `[data-path="${path}"] .add-code-comment[data-idx="${idx}"]`;
    else selector = `[data-path="${path}"] .add-code-comment[data-side="${side}"][data-idx="${idx}"]`;
    for (const el of document.querySelectorAll(selector)) el.classList.add('tw-invisible');
    $newConversationHolder.find('.dropdown').dropdown();
    initCompReactionSelector($newConversationHolder);
  } catch {
    if (generation === currentPageGeneration()) {
      console.error('error when submitting conversation', e);
      showErrorToast(window.config.i18n.network_error);
    }
  } finally {
    e.target.classList.remove('is-loading');
  }
}

async function onResolveConversation(e) {
  e.preventDefault();
  const generation = currentPageGeneration();
  const comment_id = $(this).data('comment-id');
  const origin = $(this).data('origin');
  const action = $(this).data('action');
  const url = $(this).data('update-url');
  try {
    const response = await POST(url, {data: new URLSearchParams({origin, action, comment_id})});
    const data = await response.text();
    if (generation !== currentPageGeneration()) return;
    if ($(this).closest('.conversation-holder').length) {
      const $conversation = $(data);
      $(this).closest('.conversation-holder').replaceWith($conversation);
      $conversation.find('.dropdown').dropdown();
      initCompReactionSelector($conversation);
    } else {
      window.location.reload();
    }
  } catch (error) {
    if (generation === currentPageGeneration()) console.error('Error:', error);
  }
}

function initRepoDiffConversationForm() {
  $(document).on('submit', '.conversation-holder form', onConversationSubmit);
  $(document).on('click', '.resolve-conversation', onResolveConversation);
}

function onPreviousConversation(e) {
  const $conversation = $(e.currentTarget).closest('.comment-code-cloud');
  const $conversations = $('.comment-code-cloud:not(.tw-hidden)');
  const index = $conversations.index($conversation);
  const previousIndex = index > 0 ? index - 1 : $conversations.length - 1;
  const anchor = $conversations.eq(previousIndex).find('.comment').first()[0]?.getAttribute('id');
  if (anchor) window.location.href = `#${anchor}`;
}

function onNextConversation(e) {
  const $conversation = $(e.currentTarget).closest('.comment-code-cloud');
  const $conversations = $('.comment-code-cloud:not(.tw-hidden)');
  const index = $conversations.index($conversation);
  const nextIndex = index < $conversations.length - 1 ? index + 1 : 0;
  const anchor = $conversations.eq(nextIndex).find('.comment').first()[0]?.getAttribute('id');
  if (anchor) window.location.href = `#${anchor}`;
}

export function initRepoDiffConversationNav() {
  $(document).on('click', '.previous-conversation', onPreviousConversation);
  $(document).on('click', '.next-conversation', onNextConversation);
}

export function retireRepoDiffConversationNav() {
  $(document).off('click', '.previous-conversation', onPreviousConversation);
  $(document).off('click', '.next-conversation', onNextConversation);
}

function onShowMoreFiles() {
  initRepoIssueContentHistory();
  initViewedCheckboxListenerFor();
  countAndUpdateViewedFiles();
  initImageDiff();
  htmx.process(document.body);
}

export async function loadMoreFiles(url) {
  const info = getPageData().diffFileInfo;
  const target = document.querySelector('a#diff-show-more-files');
  if (!info || target?.classList.contains('disabled') || info.isLoadingNewData) return;
  const generation = currentPageGeneration();
  info.isLoadingNewData = true;
  target?.classList.add('disabled');
  try {
    const response = await GET(url);
    const resp = await response.text();
    if (generation !== currentPageGeneration()) return;
    const $resp = $(resp);
    $('#diff-incomplete').replaceWith($resp.find('#diff-file-boxes').children());
    $('body').append($resp.find('script#diff-data-script'));
    onShowMoreFiles();
  } catch (error) {
    if (generation === currentPageGeneration()) {
      console.error('Error:', error);
      showErrorToast('An error occurred while loading more files.');
    }
  } finally {
    target?.classList.remove('disabled');
    if (getPageData().diffFileInfo) getPageData().diffFileInfo.isLoadingNewData = false;
  }
}

function onShowMoreClick(e) {
  e.preventDefault();
  loadMoreFiles(e.target.getAttribute('data-href'));
}

async function onDiffLoadClick(e) {
  e.preventDefault();
  const generation = currentPageGeneration();
  const $target = $(e.target);
  if (e.target.classList.contains('disabled')) return;
  e.target.classList.add('disabled');
  const url = $target.data('href');
  try {
    const response = await GET(url);
    const resp = await response.text();
    if (!resp || generation !== currentPageGeneration()) return;
    $target.parent().replaceWith($(resp).find('#diff-file-boxes .diff-file-body .file-body').children());
    onShowMoreFiles();
  } catch (error) {
    if (generation === currentPageGeneration()) console.error('Error:', error);
  } finally {
    e.target.classList.remove('disabled');
  }
}

function initRepoDiffShowMore() {
  $(document).on('click', 'a#diff-show-more-files', onShowMoreClick);
  $(document).on('click', 'a.diff-load-button', onDiffLoadClick);
}

export function initRepoDiffView() {
  if (diffAttached) return;
  diffAttached = true;
  initRepoDiffConversationForm();
  if (!$('#diff-file-boxes').length) return;
  initDiffFileTree();
  initDiffCommitSelect();
  initRepoDiffShowMore();
  initRepoDiffReviewButton();
  initRepoDiffFileViewToggle();
  initViewedCheckboxListenerFor();
  initExpandAndCollapseFilesButton();
}

export function retireRepoDiffView() {
  $(document).off('submit', '.conversation-holder form', onConversationSubmit);
  $(document).off('click', '.resolve-conversation', onResolveConversation);
  $(document).off('click', 'a#diff-show-more-files', onShowMoreClick);
  $(document).off('click', 'a.diff-load-button', onDiffLoadClick);
  $(document).off('click', 'button[name="pending_review"]', onReviewButtonClick);
  retireRepoDiffConversationNav();
  for (const el of toggleEls) el.removeEventListener('click', onFileViewToggle);
  toggleEls.clear();
  reviewForms.clear();
  retireDiffFileTree();
  retireDiffCommitSelect();
  retireViewedFiles();
  retireImageDiff();
  diffAttached = false;
}
