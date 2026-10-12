import {svg} from '../svg.js';
import {toggleElem} from '../utils/dom.js';
import {pathEscapeSegments} from '../utils/url.js';
import {GET} from '../modules/fetch.js';
import {contributionRootSelector, currentPageGeneration} from '../native-lifecycle.js';

const threshold = 50;
let files = [];
let repoFindFileInput, repoFindFileTableBody, repoFindFileNoResult;

// return the case-insensitive sub-match result as an array:  [unmatched, matched, unmatched, matched, ...]
// res[even] is unmatched, res[odd] is matched, see unit tests for examples
// argument subLower must be a lower-cased string.
export function strSubMatch(full, subLower) {
  const res = [''];
  let i = 0, j = 0;
  const fullLower = full.toLowerCase();
  while (i < subLower.length && j < fullLower.length) {
    if (subLower[i] === fullLower[j]) {
      if (res.length % 2 !== 0) res.push('');
      res[res.length - 1] += full[j];
      j++;
      i++;
    } else {
      if (res.length % 2 === 0) res.push('');
      res[res.length - 1] += full[j];
      j++;
    }
  }
  if (i !== subLower.length) {
    // if the sub string doesn't match the full, only return the full as unmatched.
    return [full];
  }
  if (j < full.length) {
    // append remaining chars from full to result as unmatched
    if (res.length % 2 === 0) res.push('');
    res[res.length - 1] += full.substring(j);
  }
  return res;
}

export function calcMatchedWeight(matchResult) {
  let weight = 0;
  for (let i = 0; i < matchResult.length; i++) {
    if (i % 2 === 1) { // matches are on odd indices, see strSubMatch
      // use a function f(x+x) > f(x) + f(x) to make the longer matched string has higher weight.
      weight += matchResult[i].length * matchResult[i].length;
    }
  }
  return weight;
}

export function filterRepoFilesWeighted(files, filter) {
  let filterResult = [];
  if (filter) {
    const filterLower = filter.toLowerCase();
    // TODO: for large repo, this loop could be slow, maybe there could be one more limit:
    // ... && filterResult.length < threshold * 20,  wait for more feedbacks
    for (let i = 0; i < files.length; i++) {
      const res = strSubMatch(files[i], filterLower);
      if (res.length > 1) { // length==1 means unmatched, >1 means having matched sub strings
        filterResult.push({matchResult: res, matchWeight: calcMatchedWeight(res)});
      }
    }
    filterResult.sort((a, b) => b.matchWeight - a.matchWeight);
    filterResult = filterResult.slice(0, threshold);
  } else {
    for (let i = 0; i < files.length && i < threshold; i++) {
      filterResult.push({matchResult: [files[i]], matchWeight: 0});
    }
  }
  return filterResult;
}

function filterRepoFiles(filter) {
  if (!repoFindFileInput || !repoFindFileTableBody) return;
  const treeLink = repoFindFileInput.getAttribute('data-url-tree-link');
  repoFindFileTableBody.innerHTML = '';

  const filterResult = filterRepoFilesWeighted(files, filter);

  toggleElem(repoFindFileNoResult, !filterResult.length);
  for (const r of filterResult) {
    const row = document.createElement('tr');
    const cell = document.createElement('td');
    const a = document.createElement('a');
    a.setAttribute('href', `${treeLink}/${pathEscapeSegments(r.matchResult.join(''))}`);
    a.innerHTML = svg('octicon-file', 16, 'tw-mr-2');
    row.append(cell);
    cell.append(a);
    for (const [index, part] of r.matchResult.entries()) {
      const span = document.createElement('span');
      // safely escape by using textContent
      span.textContent = part;
      // if the target file path is "abc/xyz", to search "bx", then the matchResult is ['a', 'b', 'c/', 'x', 'yz']
      // the matchResult[odd] is matched and highlighted to red.
      if (index % 2 === 1) span.classList.add('ui', 'text', 'red');
      a.append(span);
    }
    repoFindFileTableBody.append(row);
  }
}

let activeInput = null;
let activeGeneration = -1;

function onFindFileInput() {
  if (!repoFindFileInput || repoFindFileInput !== activeInput) return;
  filterRepoFiles(repoFindFileInput.value);
}

async function loadRepoFiles(input, generation) {
  const response = await GET(input.getAttribute('data-url-data-link'));
  const loaded = await response.json();
  // Async retirement: drop the result when the page retired or the input
  // was replaced while the request was in flight.
  if (generation !== currentPageGeneration() || activeInput !== input || !input.isConnected) return;
  files = loaded;
  filterRepoFiles(input.value);
}

export function initFindFileInRepo() {
  const input = document.getElementById('repo-file-find-input');
  if (!input) return;
  if (input.closest(contributionRootSelector)) return;
  if (activeInput === input) return;
  retireFindFileInRepo();
  repoFindFileInput = input;
  activeInput = input;
  activeGeneration = currentPageGeneration();
  repoFindFileTableBody = document.querySelector('#repo-find-file-table tbody');
  repoFindFileNoResult = document.getElementById('repo-find-file-no-result');
  input.addEventListener('input', onFindFileInput);
  loadRepoFiles(input, activeGeneration);
}

export function retireFindFileInRepo() {
  if (activeInput) activeInput.removeEventListener('input', onFindFileInput);
  activeInput = null;
  activeGeneration = -1;
  repoFindFileInput = null;
  repoFindFileTableBody = null;
  repoFindFileNoResult = null;
  files = [];
}
