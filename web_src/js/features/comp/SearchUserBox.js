import $ from 'jquery';
import {htmlEscape} from 'escape-goat';

const {appSubUrl} = window.config;
const looksLikeEmailAddressCheck = /^\S+@\S+$/;

// The Fomantic search widget owns its DOM/listeners once initialized; track
// the box so retire destroys exactly the widget init created (mirrors
// org-team.js). Repeated init on the same box is a no-op.
let attachedBox = null;

export function retireCompSearchUserBox() {
  if (!attachedBox) return;
  if (typeof $.fn.search === 'function') {
    $(attachedBox).search('destroy');
  }
  attachedBox = null;
}

export function initCompSearchUserBox() {
  const searchUserBox = document.getElementById('search-user-box');
  if (!searchUserBox) return;
  if (attachedBox === searchUserBox) return;
  retireCompSearchUserBox();

  const allowEmailInput = searchUserBox.getAttribute('data-allow-email') === 'true';
  const allowEmailDescription = searchUserBox.getAttribute('data-allow-email-description') ?? undefined;
  $(searchUserBox).search({
    minCharacters: 2,
    apiSettings: {
      url: `${appSubUrl}/user/search_candidates?q={query}`,
      onResponse(response) {
        const resultItems = [];
        const searchQuery = searchUserBox.querySelector('input').value;
        const searchQueryUppercase = searchQuery.toUpperCase();
        for (const item of response.data) {
          const resultItem = {
            title: item.login,
            image: item.avatar_url,
            description: htmlEscape(item.full_name),
          };
          if (searchQueryUppercase === item.login.toUpperCase()) {
            resultItems.unshift(resultItem); // add the exact match to the top
          } else {
            resultItems.push(resultItem);
          }
        }

        if (allowEmailInput && !resultItems.length && looksLikeEmailAddressCheck.test(searchQuery)) {
          const resultItem = {
            title: searchQuery,
            description: allowEmailDescription,
          };
          resultItems.push(resultItem);
        }

        return {results: resultItems};
      },
    },
    searchFields: ['login', 'full_name'],
    showNoResults: false,
  });
  attachedBox = searchUserBox;
}
