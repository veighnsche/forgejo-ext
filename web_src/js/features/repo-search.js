import {contributionRootSelector, navigateNative} from '../native-lifecycle.js';

let searchController = null;

export function retireRepositorySearch() {
  searchController?.abort();
  searchController = null;
}

export function initRepositorySearch() {
  retireRepositorySearch();
  const repositorySearchForm = document.querySelector('#repo-search-form');
  if (!repositorySearchForm || repositorySearchForm.closest(contributionRootSelector)) return;
  searchController = new AbortController();
  const {signal} = searchController;

  repositorySearchForm.addEventListener('change', (e) => {
    e.preventDefault();

    const formData = new FormData(repositorySearchForm);
    const params = new URLSearchParams(formData);

    if (e.target.name === 'clear-filter') {
      params.delete('archived');
      params.delete('fork');
      params.delete('mirror');
      params.delete('template');
      params.delete('private');
    }

    params.delete('clear-filter');
    const url = new URL(window.location.href);
    url.search = params.toString();
    navigateNative(url.href);
  }, {signal});
}
