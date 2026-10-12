import {vi} from 'vitest';
import {GET} from '../modules/fetch.js';
import {retirePageFeatures} from '../native-lifecycle.js';
import {initCommonIssueListQuickGoto, parseIssueListQuickGotoLink, retireCommonIssueListQuickGoto} from './common-issue-list.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn()}));

afterEach(() => {
  retireCommonIssueListQuickGoto();
  document.body.innerHTML = '';
  window.history.replaceState(null, '', '/');
});

test('parseIssueListQuickGotoLink', () => {
  expect(parseIssueListQuickGotoLink('/link', '')).toEqual('');
  expect(parseIssueListQuickGotoLink('/link', 'abc')).toEqual('');
  expect(parseIssueListQuickGotoLink('/link', '123')).toEqual('/link/issues/123');
  expect(parseIssueListQuickGotoLink('/link', '#123')).toEqual('/link/issues/123');
  expect(parseIssueListQuickGotoLink('/link', 'owner/repo#123')).toEqual('');

  expect(parseIssueListQuickGotoLink('', '')).toEqual('');
  expect(parseIssueListQuickGotoLink('', 'abc')).toEqual('');
  expect(parseIssueListQuickGotoLink('', '123')).toEqual('');
  expect(parseIssueListQuickGotoLink('', '#123')).toEqual('');
  expect(parseIssueListQuickGotoLink('', 'owner/repo#')).toEqual('');
  expect(parseIssueListQuickGotoLink('', 'owner/repo#123')).toEqual('/owner/repo/issues/123');
});

function installQuickGoto() {
  document.body.innerHTML = `<form>
    <input name="q" value="123">
    <button id="issue-list-quick-goto" data-repo-link="/link" type="button">Go</button>
  </form>`;
  return {
    form: document.querySelector('form'),
    input: document.querySelector('input[name=q]'),
    goto: document.getElementById('issue-list-quick-goto'),
  };
}

test('quick goto binds once, resolves against the current repo, and retires cleanly', async () => {
  GET.mockResolvedValue({status: 200});
  const {input, goto} = installQuickGoto();
  initCommonIssueListQuickGoto();
  initCommonIssueListQuickGoto();
  await vi.waitFor(() => expect(goto.getAttribute('data-issue-goto-link')).toBe('/link/issues/123'));
  expect(GET).toHaveBeenCalledTimes(1);
  expect(GET).toHaveBeenCalledWith('/link/issues/123/info');

  retireCommonIssueListQuickGoto();
  GET.mockClear();
  input.value = '456';
  input.dispatchEvent(new Event('input'));
  await new Promise((resolve) => setTimeout(resolve, 350));
  expect(GET).not.toHaveBeenCalled();
});

test('a check that finishes after retirement cannot publish a stale goto link', async () => {
  let resolveGet;
  GET.mockImplementation(() => new Promise((resolve) => { resolveGet = resolve }));
  const {input, goto} = installQuickGoto();
  initCommonIssueListQuickGoto();
  // The immediate check for "123" is in flight; retire the page, then let
  // the old repository's answer arrive.
  retireCommonIssueListQuickGoto();
  retirePageFeatures();
  resolveGet({status: 200});
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(goto.getAttribute('data-issue-goto-link')).toBeNull();
  expect(input.value).toBe('123');
});
