import $ from 'jquery';

const {appSubUrl} = window.config;

// The Fomantic search widget owns its DOM/listeners once initialized; track
// the box so retire destroys exactly the widget init created.
let attachedBox = null;

export function initOrgTeamSearchRepoBox() {
  const $searchRepoBox = $('#search-repo-box');
  if (!$searchRepoBox.length || attachedBox === $searchRepoBox[0]) return;
  $searchRepoBox.search({
    minCharacters: 2,
    apiSettings: {
      url: `${appSubUrl}/repo/search?q={query}&uid=${$searchRepoBox.data('uid')}`,
      onResponse(response) {
        const items = [];
        for (const item of response.data) {
          items.push({
            title: item.repository.full_name.split('/')[1],
            description: item.repository.full_name,
          });
        }
        return {results: items};
      },
    },
    searchFields: ['full_name'],
    showNoResults: false,
  });
  attachedBox = $searchRepoBox[0];
}

export function retireOrgTeamSearchRepoBox() {
  if (!attachedBox) return;
  if (typeof $.fn.search === 'function') {
    $(attachedBox).search('destroy');
  }
  attachedBox = null;
}
