import {initRepoPullRequestCommitStatus, retireRepoPullRequestCommitStatus} from './repo-issue-pr-status.js';

afterEach(() => {
  retireRepoPullRequestCommitStatus();
  document.body.innerHTML = '';
});

function installPanel(id = 'panel') {
  document.body.innerHTML = `<div class="commit-status-panel" id="${id}">
    <button class="commit-status-hide-checks" data-show-all="Show all" data-hide-all="Hide all">Hide all</button>
    <div class="commit-status-list"></div>
  </div>`;
  return {
    btn: document.querySelector('.commit-status-hide-checks'),
    list: document.querySelector('.commit-status-list'),
  };
}

test('toggle collapses and expands the checks list once per click', () => {
  const {btn, list} = installPanel();
  initRepoPullRequestCommitStatus();
  initRepoPullRequestCommitStatus();
  btn.click();
  expect(list.style.maxHeight).toBe('0px');
  expect(btn.textContent).toBe('Show all');
  btn.click();
  expect(list.style.maxHeight).toBe('');
  expect(btn.textContent).toBe('Hide all');
});

test('retire removes the toggle handler', () => {
  const {btn, list} = installPanel();
  initRepoPullRequestCommitStatus();
  retireRepoPullRequestCommitStatus();
  btn.click();
  expect(list.style.maxHeight).toBe('');
});

test('controls inside a retained contribution root stay owner-managed', () => {
  document.body.innerHTML = `<div data-extension-panel>
      <div class="commit-status-panel">
        <button class="commit-status-hide-checks" data-show-all="Show all" data-hide-all="Hide all">Hide all</button>
        <div class="commit-status-list"></div>
      </div>
    </div>`;
  const btn = document.querySelector('.commit-status-hide-checks');
  initRepoPullRequestCommitStatus();
  btn.click();
  expect(document.querySelector('.commit-status-list').style.maxHeight).toBe('');
});
